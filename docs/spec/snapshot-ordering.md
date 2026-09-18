# Snapshot ordering and refresh

Status: accepted, 2026-09-18. Implements the local snapshot contract in ADR 0001.

## One desktop inventory stream

The desktop applies inventory snapshots only from its active `agentx serve` child. One-shot commands remain independently usable and may emit snapshots for terminal consumers. The desktop consumes their outcomes, progress and conflicts, but does not apply their snapshots to its inventory. Inventory rows and drift state come from the authoritative snapshot; individual `skill` or `drift` events may supply details or notifications but cannot overwrite those rows independently.

Each serve process creates a new `instance_id`. Its `scan_counter` starts at 1 for the initial snapshot and increases for each changed snapshot it emits. The supervisor associates events with the child that produced them. Apply snapshots only from that active child's instance and with increasing counters within it; never compare counters across instances or with one-shot commands. On restart, keep the displayed inventory marked stale until the new child's initial snapshot arrives. Ignore queued events from old children, including refresh acknowledgements.

## Refresh after a command

After a one-shot command terminates, including a partial failure or cancellation that might have changed state, the desktop sends a `refresh` request with a unique `request_id` to its active serve child. Command outcome and inventory freshness are separate states in the UI.

Serve serializes scans. A refresh requires a scan begun after the request was received. A scan already in progress cannot satisfy it. Multiple requests received before a subsequent scan begins may share that scan. The initial scan may satisfy a request only if it begins after that request.

Compute identities and drift before comparing canonical inventory data. Exclude instance ids, counters, observation times and request ids from that comparison. When the inventory changed, emit the whole snapshot first. Always emit `refresh_complete` for each satisfied request with `request_id`, `instance_id`, `ok: true` and the `scan_counter` of the current snapshot, even when the inventory is unchanged. Accept a matching acknowledgement even when its counter equals the displayed snapshot's counter. A failed or unstable scan returns `ok: false` with an error and cannot mark inventory fresh. If serve restarts, the desktop reissues pending requests to the new child with new request ids.

Serve always emits an initial whole snapshot. Later unchanged background scans emit no snapshot. Terminal mutations reach serve through the existing change signal and filesystem watches. If an operation requests fresh inventory, it uses the same refresh acknowledgement rule rather than waiting for a content-change event.

## Consistent local reads

Scans hold a shared advisory lock while reading local settings, lineage and inventory. Mutators and journal recovery hold the exclusive lock. A mutation's own post-action scan may use its already-held exclusive lock. Waiting for a lock to be free without holding it during the reads is insufficient.

If an unfinished mutation journal is found under a read lock, release it and recover under the exclusive lock before rescanning. Do not upgrade a held shared lock. Unresolved recovery reports an error; it must not emit partially applied state as fresh inventory. Lock waits and retries respect the command or refresh deadline; failure to acquire the lock reports code 7.

Network fetches and MCP handshakes stay outside the local read section. Validate that their referenced declarations or refs still match when composing the result. Agentx locks do not coordinate arbitrary editors or installers. Detect observed file changes during the scan, retry within a bound and report instability when a consistent observation cannot be obtained. Snapshots converge after external writes settle; they are not transactional filesystem snapshots. Destructive commands validate their own current inputs under the mutation-safety contract.

## Whole snapshots and targeted work

Serve may rescan affected configurations and retain the untouched subtrees of its existing whole snapshot only when invalidation identifies all affected inputs. A library or lineage change can affect multiple configurations; unknown scope falls back to a full scan. A refresh must also process outstanding watch and mutation invalidations before it is acknowledged.

Standalone commands have no previous snapshot. Commands promising a `snapshot`, including `scan --configuration`, therefore perform a full scan; the configuration is a refresh hint, not permission to label a partial graph as a whole snapshot. No inventory is persisted to support this optimization.

Fleet upload ordering uses a separate durable protocol. Neither `instance_id` nor `scan_counter` is the fleet sequence.

## Required verification

Cover an old serve event after a one-shot result, old-child events and acknowledgements after restart, refresh during an earlier scan, unchanged-content refresh completion, partial command failure, scan overlap with mutations, a crash before the mutation signal, pending journal recovery, external file changes, and standalone targeted scans. Initial snapshots must emit even when an earlier process observed identical content.
