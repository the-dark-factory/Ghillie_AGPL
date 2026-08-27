# Security

## Reporting

Report suspected vulnerabilities by email to security@thedarkfactory.co.uk.
Plain reports are fine; there is no bounty programme and we will not pretend
otherwise. We aim to acknowledge within a week. Please do not open public
issues for unpatched vulnerabilities.

## What this software claims, so you can attack it honestly

- **No inbound path**: the terminal polls outward; the sole exception is
  `-glass`, one loopback-only listener whose bind is decided by a proven
  policy front and refused fail-closed without it.
- **Nothing received is executed**: deliveries land in quarantine as
  notices; there is no installer-on-receipt and no executor.
- **Delivered bytes never gain the execute bit**: only a front locally
  built from re-proved source is marked executable, by the install path,
  after its proof discharges and its truth table checks.
- **Decisions live in proven cores**: the Go glue holds no rules; it asks
  separate decider binaries and fails closed when they are absent.
- **PII confinement**: the purchaser reference travels in the enrolment
  payload and appears in no log, report, or quarantine file.

A demonstration that any of these claims fails — a tampered bundle that
discharges, a listener where none should be, a delivered byte that runs —
is exactly the report we most want.
