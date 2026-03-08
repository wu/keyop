#!/usr/bin/env bash
set -euo pipefail

if [ "$(uname -s)" != "Darwin" ]; then
  echo "Skipping keyop-notify build: not macOS"
  exit 0
fi

APP_DIR="x/notify/cmd/notify-sender"
APP_BUNDLE="${APP_DIR}/keyop-notify.app"

mkdir -p "${APP_BUNDLE}/Contents/MacOS" "${APP_BUNDLE}/Contents/Resources"

# copy icon if provided
if [ -f "images/keyop.icns" ]; then
  cp -f "images/keyop.icns" "${APP_BUNDLE}/Contents/Resources/keyop.icns"
fi

# Prefer building the Objective-C wrapper if present
if [ -f "${APP_DIR}/objc/wrapper.m" ]; then
  clang -fobjc-arc -framework Foundation -framework UserNotifications -framework AppKit -o "${APP_BUNDLE}/Contents/MacOS/keyop-notify" "${APP_DIR}/objc/wrapper.m"
else
  go build -o "${APP_BUNDLE}/Contents/MacOS/keyop-notify" ./x/notify/cmd/notify-sender
fi

# Create Info.plist if missing
if [ ! -f "${APP_BUNDLE}/Contents/Info.plist" ]; then
  cat > "${APP_BUNDLE}/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleName</key><string>keyop-notify</string>
    <key>CFBundleDisplayName</key><string>KeyOp Notify</string>
    <key>CFBundleIdentifier</key><string>io.keyop.notify</string>
    <key>CFBundleExecutable</key><string>keyop-notify</string>
    <key>CFBundleIconFile</key><string>keyop</string>
    <key>CFBundlePackageType</key><string>APPL</string>
</dict>
</plist>
PLIST
fi
