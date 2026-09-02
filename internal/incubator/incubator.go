// Package incubator imports legacy mail corpora (Thunderbird profile dumps,
// .eml trees) into a docker-mailserver incubator mailbox via doveadm save
// (issue #252 / epic #250). It is the ETL front of the mail incubator: scan
// the corpus tree, derive the canonical Message-ID dedup key per message,
// skip already-imported messages via the state manifest
// (var/state/incubator-<label>.json), and push new ones into the owner
// mailbox — INBOX (pilot default) or a replicated folder tree.
package incubator
