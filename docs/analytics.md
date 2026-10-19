# Usage statistics

Denova release builds send optional product events directly to Umami Cloud's
`https://gateway.umami.is/api/send`, using Website ID
`23394fdd-4d21-4dd0-b833-16bfedff38d8`, `tag=app`, and the synthetic hostname
`app.denova-ai.com`. The public website uses the same Website ID and `tag=site`.
Always filter dashboards by tag; the Website overview and monthly allowance are
shared. No management API key is bundled with either application.

## User control and privacy

The first-run notice explains that collection is enabled by default. No event
is collected until the notice is dismissed. Users can turn it off immediately
or under Settings → General. Disabling cancels in-flight delivery, drops the
memory queue, and invalidates earlier execution handles. Re-enabling does not
upload earlier activity. The setting applies to the host user across Projects,
writing, games, and authenticated LAN clients.

A preference write failure also stops collection in memory and permits a save
retry; it never leaves the first-run notice blocking creation. Reading time
from before a disable/re-enable transition is discarded.

Source/development builds do not read host analytics state or send events.
Missing permissions, corrupt state, or another running process owning the host
analytics lease disable statistics without blocking creative data loading.

`os.UserConfigDir()/Denova/analytics.json` stores only the preference, notice
acknowledgement, random `app_<UUID>` identity, first active date, recent period
keys, today's bounded feature set, and three retention markers. This file is
outside portable `.denova` data and is not a Session recovery journal. No
operation history or offline event payloads are persisted. Reinstalling while
keeping this file preserves identity; deleting it starts a new observation.

Only explicitly constructed fields from `internal/analytics/events.go` cross
the network. They include fixed categories, version/platform, execution result,
integer execution duration in milliseconds, dates, and a random identity.
Manuscripts, prompts, model replies, images, names, paths, business IDs, model
names, API endpoints/keys, raw errors, trace data, and journal content are
excluded. Browser activity
requests accept only a feature enum and reject extra fields. Cloud receives the
connection IP; a persistent random ID makes this pseudonymous usage analysis,
not fully anonymous collection.

## Event catalog and metrics

There are ten event names. Every event includes `version` and `platform` in
its data, set centrally when the event is created. They describe the running
application at the time of the event, not the version or platform at first use.
The envelope includes the random local identity, timestamp, fixed Website ID,
`tag=app`, synthetic hostname, and a fixed category path such as `/app/writing`;
it never contains a real Project path or browser URL.

| Event | Admission | Additional properties |
| --- | --- | --- |
| `app.first_active` | First observed user activity per identity | `cohort_date` |
| `app.active_day` | First user activity per UTC day | None |
| `app.active_week` | First activity per ISO calendar week | None |
| `app.active_month` | First activity per calendar month | None |
| `app.retained_d1`, `app.retained_d7`, `app.retained_d30` | User activity exactly 1, 7, or 30 UTC days after first activity | `cohort_date` |
| `app.feature_active` | First effective activity in a category per UTC day | `feature` |
| `app.run_finished` | One ended Writing/Game/Chat/Automation execution or direct image request | `feature`, `runtime`, `trigger`, `outcome`, `duration_ms` |
| `app.action_completed` | Successful Book creation, Book export, or Project version restore | `action` |

DAU/WAU/MAU are the event counts of their respective period events. WAU is an
ISO week and MAU a calendar month; these are not rolling 7/30-day uniques. A
backend process, reload, empty Writing/Game page, automatic saves without
changes, tool call, or sub-Agent does not independently count as activity.
Focused visible content reading counts after 30 seconds. Changed editor saves,
user execution admission, and completed explicit actions also count.
Automation-origin Chat executions carry `trigger=automation` and do not create
user activity, retention or daily feature events.

Feature categories are `writing`, `game`, `chat`, `image`, `library`, `versions`,
`automation`, `projects` and `resources`. Feature reach is the count of
`app.feature_active` for a feature, divided by DAU over the same day. Reading
and explicit operations both qualify; one and ten qualifying visits to a
category on the same day produce one event. This measures reach, not visit
frequency, total reading time, or depth of use.

Execution results are bounded to `success`, `failure`, `cancelled`, `suspended`
and `preempted`. Runtimes are `native`, `codex` and `claude`; triggers are `user`
and `automation`. `duration_ms` is the whole execution's integer elapsed time
in milliseconds, including zero for an execution shorter than one millisecond.
Only execution events carry it. It is not model first-token latency and is
not bucketed or capped. Internal runtime continuation/tool rounds are not
independently instrumented. An execution outcome is a technical result, not a
judgement of creative quality.

`app.run_finished` describes the outcomes of executions that finish and are
successfully reported. There is no paired start event or crash report;
executions interrupted by a process crash can be missing. Do not interpret
its success ratio as the success rate of every attempted execution.

Completed actions are `book_created`, `content_exported` and `version_restored`.
`book_created` covers the Create Book entry point, not every kind of Project
creation. `content_exported` covers Book export, not every resource export.
Their activity categories are `projects`, `writing` and `versions`, respectively.

For each `cohort_date`, D7 retention is `app.retained_d7 / app.first_active`;
calculate only after the cohort has aged seven complete days. D1/D30 use the
same rule. These are first observed participating active instances, not new
downloads or natural persons. Several browsers/LAN devices share a host identity.
The website's download click does not prove download completion, and no identity
is transferred between website and App.

D7 means a return on exactly the seventh UTC day, not any return within seven
days. Review it alongside weekly/monthly activity for intermittent creative
work. Compare cohorts by their first-active date; filtering return events by
their current `version` does not produce retention by first-use version.

Umami's default visitor/retention implementation uses sessions, which can split
after network changes or salt rotation. Use the deduplicated events above for
product metrics instead of treating Website visitors as App users. Use event
property breakdowns or dashboard exports for cohort calculations.

## Delivery and verification

The queue holds at most 128 events in memory, with a five-second HTTP deadline.
Shutdown cancels delivery and waits at most one second. Delivery is best effort:
counter markers are saved before sending, and ambiguous/failed responses are
not retried to avoid duplicating count events. Offline activity is not backfilled.
Reported statistics therefore represent samples successfully received by Cloud.
First-active and return events can be lost independently, so cohort ratios are
sample estimates rather than exact population retention.
Collection never delays an Agent's model execution on a network request.

The host `analytics.json` format and managed `.denova` data are unchanged by
this event catalog. No operation history, execution identifiers, offline event
store, or additional configuration is introduced.

The fixed `Mozilla/5.0 Denova/1.0` User-Agent was verified with Cloud. Bare
`Denova/1.0` is filtered as a bot despite HTTP 200. The exporter requires a
collection receipt with `cache` and `sessionId`; HTTP 200 alone is insufficient.
Real smoke checks use a synthetic identity and `tag=app-test`, excluded from
production dashboards. Automated tests use temporary host state and local HTTP
collectors; E2E source binaries stay off Cloud and the real user's state.

Umami's free plan currently retains six months and includes 100,000 monthly
units. Website pageviews, App custom events and custom event properties all
consume this shared allowance. Period and retention events are not sampled;
reduce high-frequency execution detail first if the allowance becomes tight.
