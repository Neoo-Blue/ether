# SubDL subtitles

Searches SubDL by IMDb ID and downloads an SRT only after a viewer selects it. The owner supplies their own SubDL API key; no credentials are included. The plugin connects to `api.subdl.com` for search and `dl.subdl.com` for downloads.

## Setup

Create your own API key in your SubDL account, install the package in Ether, and enter the key in its settings. Enable SubDL under Playback > Audio and subtitles. Select a subtitle in your player to download it.

Screenshots to add: the SubDL integration settings drawer; a subtitle selection in a player.

## Limits

Search requires an IMDb ID. Only single-SRT ZIP archives and the first 30 results are supported; season packs and other formats are skipped at download. SubDL controls availability, rate limits and file contents. Its terms and API limits apply to your account. Third-party subtitles may have their own rights. The bundled fflate 0.8.2 is MIT licensed; see `vendor/LICENSE`.

To rebuild the bundled entry from the repository root, run `go run ./sdk/examples/subdl/build`.
