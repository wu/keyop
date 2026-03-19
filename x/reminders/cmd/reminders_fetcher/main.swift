#!/usr/bin/env swift
// reminders_fetcher - small utility to fetch Reminders via EventKit and print JSON per line
// Usage: reminders_fetcher [--list LISTNAME] [--only-uncompleted]

import Foundation
import EventKit

// Simple argument parsing
var listName: String? = nil
var onlyUncompleted = false
var timeoutSeconds = 10

let args = CommandLine.arguments
var i = 1
while i < args.count {
    let a = args[i]
    switch a {
    case "--list":
        if i + 1 < args.count {
            listName = args[i + 1]
            i += 2
        } else {
            fputs("ERROR: --list requires a value\n", stderr)
            exit(1)
        }
    case "--only-uncompleted":
        onlyUncompleted = true
        i += 1
    case "--timeout":
        if i + 1 < args.count, let v = Int(args[i + 1]) {
            timeoutSeconds = v
            i += 2
        } else {
            fputs("ERROR: --timeout requires an integer value\n", stderr)
            exit(1)
        }
    default:
        i += 1
    }
}

let store = EKEventStore()
let sem = DispatchSemaphore(value: 0)
var gotAccess = false

// Request access using dynamic selector calls to remain compatible with older SDKs
let anyStore = store as AnyObject
let block: @convention(block) (Bool, Error?) -> Void = { granted, error in
    if let err = error {
        fputs("ERROR: request access error: \(err)\n", stderr)
    }
    gotAccess = granted
    sem.signal()
}
let blockObj = unsafeBitCast(block, to: AnyObject.self)

// Prefer the macOS 14 selector name if available at runtime; otherwise call the older selector
let newSel = NSSelectorFromString("requestFullAccessToRemindersWithCompletion:")
let oldSel = NSSelectorFromString("requestAccessToEntityType:completion:")
if anyStore.responds(to: newSel) {
    _ = anyStore.perform(newSel, with: blockObj)
} else if anyStore.responds(to: oldSel) {
    let entity = NSNumber(value: EKEntityType.reminder.rawValue)
    _ = anyStore.perform(oldSel, with: entity, with: blockObj)
} else {
    fputs("ERROR: no request-access selector available\n", stderr)
    sem.signal()
}

let waitResult = sem.wait(timeout: .now() + .seconds(timeoutSeconds))
if waitResult == .timedOut {
    fputs("ERROR: requestAccess timed out\n", stderr)
    exit(2)
}
if !gotAccess {
    fputs("ERROR: Reminders access not granted\n", stderr)
    exit(3)
}

let calendars = store.calendars(for: .reminder)
var targetCalendars: [EKCalendar] = []
if let list = listName {
    targetCalendars = calendars.filter { $0.title == list }
    if targetCalendars.isEmpty {
        fputs("ERROR: List named '\(list)' not found\n", stderr)
        exit(4)
    }
} else {
    targetCalendars = calendars
}

let predicate = store.predicateForReminders(in: targetCalendars)

let group = DispatchGroup()
group.enter()

store.fetchReminders(matching: predicate) { reminders in
    if let reminders = reminders {
        let isoFmt = ISO8601DateFormatter()
        for r in reminders {
            // Optionally skip completed
            if onlyUncompleted && (r.isCompleted) {
                continue
            }

            var dict: [String: Any] = [:]
            dict["id"] = r.calendarItemIdentifier
            dict["title"] = r.title ?? ""
            dict["note"] = r.notes ?? ""
            dict["done"] = r.isCompleted
            dict["calendar"] = r.calendar.title

            // due date
            if let comps = r.dueDateComponents, let dueDate = Calendar.current.date(from: comps) {
                dict["due"] = isoFmt.string(from: dueDate)
                dict["due_has_time"] = comps.hour != nil
            }
            // scheduled / start date
            if let comps = r.startDateComponents, let startDate = Calendar.current.date(from: comps) {
                dict["scheduled"] = isoFmt.string(from: startDate)
            }
            // creation & last modified (EKCalendarItem provides creationDate and lastModifiedDate)
            if let created = r.creationDate {
                dict["created"] = isoFmt.string(from: created)
            }
            if let modified = r.lastModifiedDate {
                dict["updated"] = isoFmt.string(from: modified)
            }
            // completion date
            if let completion = r.completionDate {
                dict["completedAt"] = isoFmt.string(from: completion)
            }
            // priority
            dict["priority"] = r.priority

            // Extract tags from notes as hashtags (#tag)
            if let notes = r.notes {
                let ns = notes as NSString
                if let regex = try? NSRegularExpression(pattern: "#(\\w+)", options: []) {
                    let matches = regex.matches(in: notes, options: [], range: NSRange(location: 0, length: ns.length))
                    let tags = matches.map { m -> String in
                        return ns.substring(with: m.range(at: 1))
                    }
                    if !tags.isEmpty {
                        dict["tags"] = tags.joined(separator: ",")
                    }
                }
            }

            // EKReminder does not reliably expose a 'flag' KVC key; skip attempting value(forKey:)

            // Try to extract calendar color via calendar.cgColor if available
            if let cg = r.calendar.cgColor {
                if let comps = cg.components, comps.count >= 3 {
                    let rComp = Int((comps[0] * 255.0).rounded())
                    let gComp = Int((comps[1] * 255.0).rounded())
                    let bComp = Int((comps[2] * 255.0).rounded())
                    dict["color"] = String(format: "#%02X%02X%02X", rComp, gComp, bComp)
                }
            }

            // recurrence rules -> provide recurrenceDays and recurrenceX if present
            if let rules = r.recurrenceRules, rules.count > 0 {
                var recDays: [String] = []
                var recX = 0
                for rule in rules {
                    recX = max(recX, rule.interval)
                    if let days = rule.daysOfTheWeek {
                        for d in days {
                            // EKWeekday: 1 = Sunday, 2 = Monday, ... 7 = Saturday
                            let dow = d.dayOfTheWeek
                            var name = ""
                            switch dow {
                            case .monday:
                                name = "mon"
                            case .tuesday:
                                name = "tue"
                            case .wednesday:
                                name = "wed"
                            case .thursday:
                                name = "thu"
                            case .friday:
                                name = "fri"
                            case .saturday:
                                name = "sat"
                            case .sunday:
                                name = "sun"
                            @unknown default:
                                name = ""
                            }
                            if name != "" {
                                recDays.append(name)
                            }
                        }
                    }
                }
                if !recDays.isEmpty {
                    dict["recurrenceDays"] = recDays.joined(separator: ",")
                }
                dict["recurrenceX"] = recX
            }

            // JSON encode
            if JSONSerialization.isValidJSONObject(dict) {
                if let json = try? JSONSerialization.data(withJSONObject: dict, options: []) {
                    if let s = String(data: json, encoding: .utf8) {
                        print(s)
                    }
                }
            }
        }
    }
    group.leave()
}

let fetchWait = group.wait(timeout: .now() + .seconds(timeoutSeconds))
if fetchWait == .timedOut {
    fputs("ERROR: fetchReminders timed out\n", stderr)
    exit(5)
}

exit(0)

