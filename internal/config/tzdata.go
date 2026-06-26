package config

// Embed the IANA timezone database so named digest timezones (e.g.
// Europe/Kyiv) resolve on the FROM scratch runtime image, which ships no
// /usr/share/zoneinfo and no /etc/localtime. Without this, time.LoadLocation
// can only resolve UTC, so any other digest.timezone — and even the doctor /
// config validators that share this package — would fail at startup. Costs
// ~450KB in each binary that imports config. See issue #120.
import _ "time/tzdata"
