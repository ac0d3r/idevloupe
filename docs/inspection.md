# How inspection works

iDevLoupe connects to the iPhone over USB, opens a trusted session with the device's lockdownd through the
system's usbmuxd, reads what the iPhone reports about itself, and judges it check by check. Nothing is
installed on the phone, and nothing goes online.

## Rules

- A check passes only on positive evidence; when its data cannot be read, or reads incomplete, it is
  Unknown, never Pass.
- Every verdict comes with what it rests on; the JSON export carries every value as the device returned it.
- Each reply is matched to its request (request type, service, domain and key, AFC packet number), and a
  connection that fails is thrown away and opened again, so a misplaced reply cannot pass for another.
- What software cannot judge reliably (whether parts are genuine, warranty, carrier lock, the exterior) goes
  on the "Check by hand" list.

## Sources

| Source | What it gives |
|---|---|
| lockdown, root and other domains | Model, serial, IMEI, region, activation, Find My, supervision, battery, storage, iCloud backup and sync |
| MobileGestalt | Colors, model and other values, and the factory record, SysCfg (gone from iOS 17.4) |
| IORegistry (diagnostics_relay) | The battery's gas gauge, the serials and self tests the parts report now, the storage controller |
| Analytics log | Battery cycle counts and health iOS logged before |
| installation_proxy / MCInstall / AFC | Installed apps (jailbreak apps), configuration profiles, the afc2 service |

The phone number, ICCID and IMSI are dropped as soon as they are read and never reach a report; of iCloud
sync only whether an account is iCloud is kept, never the account's name.

## Checks

**Locks and management**: Find My (Activation Lock), activation, supervision, device enrollment and MDM,
configuration profiles.

**Parts**

- Factory record: SysCfg records the serial numbers of the battery, the front and back cameras, the display
  panel, the cover glass and the Touch ID sensor fitted at the factory; each is compared with what the part
  reports now. A difference means a replacement. A match means only that the two agree, not that the part is
  original: a new screen often has the original's serial written onto it, so a matching screen is only a hint.
- Dates of manufacture: a part's serial encodes the year, week and weekday it was made, after the supplier
  code. A part made after the device (dated by its legacy serial) cannot have been fitted at the factory. This
  needs no factory record, so it holds on every iOS version, but it cannot find a part taken from an older
  device.
- Face ID self test: code 0 from the Pearl driver passes; any other code asks for a test by hand.
- Touch ID: a sensor reporting its serial with factory pairing data passes; a self test, when there is one,
  decides.

**Identity**: model, unit type (retail, engineering, internal), the format of the serial and IMEI, and
whether lockdown, MobileGestalt and the factory record agree on serial and model; a network address unlike
the factory record's means another logic board or radio.

**System**: jailbreak apps and the afc2 service, boot arguments, internal or beta builds, and other settings
a retail unit does not have.

**Cellular**: the baseband's state and the SIM tray.

**Battery**: health is weighed against 80% (Apple's service threshold) and the rated cycles (1000 from the
iPhone 15 on, 500 before). Evidence of a replacement: a cycle count lower than one logged before, or a cell
made after the device. No evidence is not proof of a genuine battery: iOS's own verdict on its parts (Parts
and Service History) cannot be read over the cable.

## Colors and model data

Color codes mean different colors on different models. Color names, release dates and battery makers come
from 3uTools' device table (`devices_table.txt`): the file `IDEVLOUPE_DEVICES_TABLE` names, else 3uTools'
cache on this computer, else the values the program reads of it, built in as Go values (`internal/devtable`).

## Code layout

| Package | Role |
|---|---|
| `pkg/plistx` | plist framing and loosely typed values |
| `pkg/usbmux` | The usbmuxd protocol: devices, hotplug, pair records, connections to ports |
| `pkg/lockdown` | lockdownd: values, TLS sessions, starting services, pairing |
| `pkg/services` | diagnostics_relay, installation_proxy, MCInstall, AFC and other services |
| `pkg/syscfg` | Decoding the factory record |
| `pkg/device` | Opening a trusted session, reading the basics |
| `internal/inspect` | Collect → analyze → report (pure functions, tested on recorded data) |
| `internal/gui` | The UI |
| `internal/locales` | Translations and their generated code (`make i18n`) |
| `internal/devtable` | 3uTools' device table: the user's copy, or the values built in |
| `internal/probe` | Querying the IORegistry entry by entry (`-dump-ioreg`), to find new part fields |
| `internal/fakedevice` | A fake usbmuxd and iPhone for tests |

`pkg/` does not depend on the app and can be used on its own. The protocol follows go-ios, ipsw and
libimobiledevice.
