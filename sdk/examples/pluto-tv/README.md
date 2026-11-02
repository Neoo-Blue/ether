# Pluto TV

A hosted Live TV source that reads Pluto TV's public channel lineup and guide and requests an anonymous session when a channel is played. Channel numbers, categories and logos appear in Ether's Live TV guide. Ether ships no channels or account credentials with this example.

## Setup

Run `go run ./cmd/ether plugin check sdk/examples/pluto-tv` and `go run ./cmd/ether plugin pack -o /tmp/pluto.etherpkg sdk/examples/pluto-tv`. Install the package in Server > Plugins, select the region that matches the server's location (or Automatic), and add Pluto TV as a Live TV source. The package is built with the TypeScript SDK. Run the offline tests with `node --test sdk/examples/pluto-tv/tests/plugin.test.ts` and `go test ./sdk/examples`.

Screenshot placeholder: the Live TV grid with channel numbers, category names, logos and a programme detail card. A second screenshot shows the region setting in the plugin drawer.

## Limits

Pluto determines the actual lineup from the server's public IP; the region setting checks the returned region and does not bypass geographic restrictions. The example uses Pluto's public, undocumented endpoints and may break when they change. Availability, advertising, stream formats and rights vary by location. A session is requested at tune time, so playback requires Pluto's service to be reachable. Fixtures are trimmed recordings; the boot fixture has a synthetic session token.
