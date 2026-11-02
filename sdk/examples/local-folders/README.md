# Local folders

This hosted source scans a folder you grant to the plugin, matches movies and shows against Ether's catalogue, and returns local files as direct releases. Movie folders use `Title (Year)`; shows use `Show/Season 1/S01E02` filenames. Files remain in the selected folder.

## Setup

Run `go run ./cmd/ether plugin check sdk/examples/local-folders` and `go run ./cmd/ether plugin pack -o /tmp/local-folders.etherpkg sdk/examples/local-folders`. Install the package in Server > Plugins, grant a folder and select the same folder in settings. Open the Local folders plugin page and choose Scan folder. Use the title picker on unmatched folders to assign a catalogue title. The scheduled scan repeats hourly. Run offline tests with `node --test sdk/examples/local-folders/tests/plugin.test.ts` and `go test ./sdk/examples`.

Screenshot placeholder: the Local folders page with a scan button and an unmatched folder card containing a title search picker. A second screenshot shows the folder grant and setting in the plugin drawer.

## Limits

Only files under one granted root are indexed. The scanner recognizes MKV, MP4, M4V, AVI, MOV, TS and WebM, and series episodes named with `S01E02`. Automatic matching requires an exact catalogue title and, when present, year. The folder API returns at most 1,000 entries per directory; the scan rejects a directory at that limit, so split large directories. Scans are subject to Ether’s host-call and time budgets; very large trees can exceed them. Scans stop at ten directory levels. Changes are reflected after the next scan, and removing a manual match currently requires removing the plugin's stored data. Ether's existing player support determines which files can be played.
