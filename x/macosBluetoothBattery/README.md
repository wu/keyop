# bluetoothBatteryMacos

Monitors the battery level of Bluetooth input devices (keyboard, trackpad, mouse, etc.) connected to a macOS host and
publishes a metric for each device on every check interval.

## Platform

macOS only. The service uses two macOS system tools internally:

- **`ioreg`** — reads `BatteryPercent` and `DeviceAddress` (Bluetooth MAC) from the
  `AppleDeviceManagementHIDEventService` IOKit class.
- **`system_profiler SPBluetoothDataType`** — maps each Bluetooth MAC address to the human-readable device name shown in
  System Settings.

## Configuration

| Key              | Required | Default                  | Description                                                                                          |
|------------------|----------|--------------------------|------------------------------------------------------------------------------------------------------|
| `metric_prefix`  | No       | `{service_name}.battery` | Prefix applied to all auto-generated metric names.                                                   |
| `device_metrics` | No       | _(none)_                 | Map of device name → explicit metric name. Overrides the auto-generated name for the listed devices. |

### `pubs`

| Channel   | Required | Description                                             |
|-----------|----------|---------------------------------------------------------|
| `metrics` | Yes      | Channel to which battery metric messages are published. |

## Metric names

By default, each device's metric name is:

```
{metric_prefix}.{sanitized_device_name}
```

where `sanitized_device_name` is the device name lowercased with all non-alphanumeric characters replaced by
underscores (e.g. `Alex's Magic Trackpad` → `alex_s_magic_trackpad`).

Use `device_metrics` to assign an explicit metric name to any device.

### Finding the exact device name

Device names come from `system_profiler` and may contain Unicode curly apostrophes (e.g. `Alex's`). The plugin
normalises these to plain ASCII apostrophes automatically, so you can use a normal apostrophe in your config. To see the
names of your connected and known devices, run:

```zsh
system_profiler SPBluetoothDataType | grep -E '^\s{10}\S.*:$'
```

## Example config

```yaml
x: macosBluetoothBattery
freq: 5m

config:
  device_metrics:
    "Dude's Magic Trackpad": battery.navi-trackpad

  # fallback prefix for unmapped devices
  metric_prefix: battery.{{.ShortHostname}}

pubs:
  metrics:
    name: metrics

```

With this config a Magic Keyboard at 78% and a Magic Trackpad at 55% would produce two messages:

```json
{
  "metricName": "home.mac.keyboard.battery",
  "metric": 78,
  "text": "Magic Keyboard battery: 78%",
  ...
}
{
  "metricName": "home.mac.trackpad.battery",
  "metric": 55,
  "text": "Alex's Magic Trackpad battery: 55%",
  ...
}
```

Any connected Bluetooth device that has a battery **and** is not listed in `device_metrics` will be published using the
auto-generated name:

```
home.mac.battery.airpods_pro   →  62
```

## Notes

- Devices that do not report a battery level (e.g. speakers, phones) are silently skipped.
- If a device is found in `ioreg` but is not listed in `system_profiler` (rare), its Bluetooth MAC address is used as
  the device name (and metric name suffix) as a fallback.
- The service only runs on macOS. On other platforms `Check()` returns an error immediately.

