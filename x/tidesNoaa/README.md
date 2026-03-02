# tidesNoaa

Fetches NOAA tidal predictions, detects high and low tides, and sends alerts
when tides are at their peaks or approaching extreme levels relative to recent
history.

## Configuration

```yaml
x: tidesNoaa
freq: 15m

config:
   # NOAA CO-OPS station ID (required).
   # Find your station at https://tidesandcurrents.noaa.gov/
   stationId: "9414290"   # San Francisco, CA

   # Directory where daily tide cache files are stored (optional).
   # Default: ~/.keyop/tides
   # dataDir: /var/lib/keyop/tides
```

| Field       | Required | Default          | Description                          |
|-------------|----------|------------------|--------------------------------------|
| `stationId` | yes      | —                | NOAA CO-OPS station ID               |
| `dataDir`   | no       | `~/.keyop/tides` | Directory for daily YAML cache files |

`freq` controls how often `Check()` runs. 15 minutes is a reasonable default;
the NOAA data is at 6-minute resolution so finer than that offers no benefit.

## Data files

Each calendar day's predictions are stored as a YAML file under
`<dataDir>/<stationId>/YYYY-MM-DD.yaml`, containing the station ID, fetch
timestamp, and an array of `TideRecord` values at 6-minute intervals.

```
~/.keyop/tides/
└── 9414290/
    ├── 2026-02-28.yaml
    ├── 2026-03-01.yaml   ← today
    ├── 2026-03-02.yaml
    └── ...               ← up to 10 days ahead
```

On startup the service fetches today through the next 10 days. Files are
refreshed according to the following staleness policy:

| Day offset        | Policy                                      |
|-------------------|---------------------------------------------|
| Past (`< 0`)      | Never re-fetched — historical data is fixed |
| Today (`0`)       | Re-fetched if more than 1 hour old          |
| Tomorrow (`+1`)   | Re-fetched if more than 1 hour old          |
| Day +2 and beyond | Not re-fetched once written                 |

Today and tomorrow are refreshed hourly because NOAA incorporates real-time
corrections (atmospheric pressure, storm surge) into its near-term forecasts.
Day +2 and beyond use pure harmonic (astronomical) predictions that don't
change meaningfully hour to hour.

## Events

### `tide`

Sent on every `Check()` with the current water level and direction.

```json
{
   "event": "tide",
   "summary": "Tide: 4.21 ft rising",
   "metric": 4.21,
   "metricName": "tide.9414290",
   "data": {
      "stationId": "9414290",
      "current": {
         "time": "2026-03-01 14:00",
         "value": 4.21
      },
      "next": {
         "time": "2026-03-01 14:06",
         "value": 4.35
      },
      "state": "rising",
      "nextPeak": {
         "time": "2026-03-01 16:24",
         "value": 5.80,
         "type": "high"
      }
   }
}
```

### `high_tide_alert` / `low_tide_alert`

Sent once per tidal peak (high or low). The service scans a small lookback
window on every `Check()` so that a peak is never silently missed even if the
scheduler runs a few minutes late.

Each peak is alerted exactly once. Alerted peaks are tracked by `(type, time)`
and pruned after 24 hours, so deduplication survives service restarts.

```json
{
   "event": "high_tide_alert",
   "summary": "High tide: 5.80 ft",
   "metric": 5.80,
   "metricName": "tide.9414290",
   "data": {
      "stationId": "9414290",
      "peak": {
         "time": "2026-03-01 16:24",
         "value": 5.80,
         "type": "high"
      }
   }
}
```

### `extreme_tide`

Sent when the tide status changes relative to any of the three rolling
historical windows (1, 3, and 12 lunar cycles). Only **status changes** trigger
a message — the event is not repeated on every `Check()`.

**`status: "warning"`** is sent for a given window when all three conditions
hold simultaneously:

1. The next predicted peak would exceed that window's historical high (or fall
   below its historical low).
2. The tide is currently moving *toward* that peak — rising toward an extreme
   high, or falling toward an extreme low.
3. The window was not already in `"warning"` state.

**`status: "ok"`** is sent when the window transitions back out of warning —
i.e. the next peak is no longer extreme for that window.

```json
{
   "event": "extreme_tide",
   "status": "warning",
   "summary": "Extreme 1-lunar-cycle high tide rising: 6.20 ft",
   "metric": 6.20,
   "data": {
      "stationId": "9414290",
      "window": "1-lunar-cycle",
      "peak": {
         "time": "2026-03-01 16:24",
         "value": 6.20,
         "type": "high"
      },
      "previous": {
         "value": 5.95,
         "time": "2026-02-14 09:12"
      }
   }
}
```

A separate `extreme_tide` message is sent for each window whose status changes,
so up to three messages may be sent in a single `Check()` call.

## Historical extremes

For each rolling window the service tracks the highest and lowest predicted
water levels seen across all cached day files. Windows are expressed in lunar
cycles. One lunar cycle is defined as **28 days** — slightly shorter than the
astronomical synodic period (~29.5 days) so that a record from the equivalent
tidal phase last month always falls outside the 1-cycle window by the time the
same phase recurs this month. This prevents last month's extreme from
perpetually suppressing an alert for this month's equivalent tide.

| Window            | Duration | What it tracks                        |
|-------------------|----------|---------------------------------------|
| `1-lunar-cycle`   | 28 days  | Record high/low over the past month   |
| `3-lunar-cycles`  | 84 days  | Record high/low over the past quarter |
| `12-lunar-cycles` | 336 days | Record high/low over the past year    |

The leaderboard for each window is updated whenever a past day's file is
fetched and is fully rebuilt from the day file cache on startup (and once per
day thereafter) so that a deleted or corrupt state file converges immediately
rather than taking months to rebuild.

## Peak detection

`nextPeak` scans forward from the current record to find the next direction
reversal. `recentPeaks` scans a short lookback window (2 records behind
current) to catch any peak that the scheduler may have stepped past.

Both functions handle **plateaus** — runs of equal consecutive values — by
treating the entire flat segment as one extremum. The first record of the
plateau is reported as the peak time. A plateau never produces more than one
peak event.

## State persistence

The following are persisted to the keyop state store across restarts:

| Key                        | Contents                                       |
|----------------------------|------------------------------------------------|
| `<name>.extremes`          | High/low leaderboards for all three windows    |
| `<name>.alertedPeaks`      | Recently alerted peaks (pruned after 24 hours) |
| `<name>.extremeTideStatus` | Current warning/ok status per window           |

## NOAA CO-OPS API

```
GET https://api.tidesandcurrents.noaa.gov/api/prod/datagetter
    ?product=predictions
    &station=<stationId>
    &begin_date=<YYYYMMDD>
    &end_date=<YYYYMMDD>
    &datum=MLLW
    &time_zone=lst_ldt
    &interval=6
    &units=english
    &format=json
```

Results are in local standard/daylight time (`lst_ldt`), MLLW datum, English
units (feet), at 6-minute intervals. Find station IDs at
<https://tidesandcurrents.noaa.gov/>.




