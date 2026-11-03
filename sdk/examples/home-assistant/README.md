# Home Assistant

Use playback events to call a Home Assistant service, read entity states, or propose a scene from Ask Ether. Ether asks for approval before running the scene tool.

## Setup

Install the package as a remote plugin in Ether. Configure the Home Assistant URL and a long-lived access token in its settings. Enter mappings such as `scene.turn_on scene.movie` for each event you want to handle. Start this script on a machine that can reach Home Assistant with Python 3.9 or later and the bundled Ether Python SDK:

```sh
ETHER_URL=https://ether.example python3 sdk/examples/home-assistant/plugin.py
```

Create a pairing code in the plugin drawer and enter it at the prompt. A fresh code is needed after restarting unless you securely supply a previously obtained tunnel credential through `ETHER_PLUGIN_CREDENTIAL`. Keep both the tunnel credential and Home Assistant token private; use TLS across untrusted networks.

Screenshots to add: plugin settings with playback mappings; Ask Ether approval card for a scene.

## Limits

Home Assistant must be reachable from the sidecar. Each event maps to one service and entity. Delivery is at least once; a service may run more than once after a retry. State results are capped at 100 entities. Home Assistant controls its own service permissions and availability. Ether does not ship a Home Assistant account or token.
