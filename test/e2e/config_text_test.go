//go:build e2e

package e2e

import (
	"fmt"
	"time"
)

// baseConfig has three channels (ntfy, Bark, PagerDuty) and three levels. The page level holds in
// quiet hours; the urgent level never does. %NTFY%, %BARK% and %PD% are the fakes' addresses.
const baseConfig = `version: 1
deadline: 10s
channels:
  phone:
    type: ntfy
    server: %NTFY%
    topic: { secret: ntfy-topic }
    token: { secret: ntfy-token }
  bell:
    type: bark
    server: %BARK%
    device_key: { secret: bark-key }
  pager:
    type: pagerduty
    events_url: %PD%/v2/enqueue
    routing_key: { secret: pd-key }
levels:
  - name: note
    use_when: "FYI. Nothing is wrong."
    ask: optional
    send:
      - { channel: phone, interruption: passive }
      - { channel: bell, interruption: passive }
  - name: page
    use_when: "Needs a human soon."
    quiet_hours: hold
    send:
      - { channel: phone, interruption: active }
      - { channel: bell, interruption: time-sensitive }
      - { channel: pager, interruption: time-sensitive }
  - name: urgent
    use_when: "Production is down."
    send:
      - { channel: phone, interruption: time-sensitive }
      - { channel: bell, interruption: critical }
      - { channel: pager, interruption: critical }
`

// shimBlock maps the old paging script onto the page and note levels.
const shimBlock = `shim:
  default: { level: page, ask: "Look at it now." }
  low: { level: note, ask: "Look when you can." }
  agent: orchestrator
`

// quietBlock is a quiet-hours window in UTC that holds the sandbox clock: it began an hour or more
// ago and ends at end, two to three hours after the clock. It returns the block and end.
func quietBlock(at time.Time) (block string, end time.Time) {
	hour := at.Truncate(time.Hour)
	end = hour.Add(3 * time.Hour)
	return fmt.Sprintf("quiet_hours:\n  timezone: UTC\n  windows:\n    - { days: [], from: %q, to: %q }\n",
		hour.Add(-time.Hour).Format("15:04"), end.Format("15:04")), end
}
