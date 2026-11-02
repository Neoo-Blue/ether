import { serve } from "../../../typescript/src/serve.ts";
import { helloPlugin } from "./plugin.ts";

// Hot-path contributions require the hosted runtime. A remote installation
// offers the same HTTP routes but does not advertise those contributions.
if (helloPlugin.manifest.ether) {
  const ether = helloPlugin.manifest.ether;
  ether.runtime = { kind: "remote" };
  ether.capabilities = ether.capabilities?.filter(
    (capability) => capability !== "ranking_step" && capability !== "rule_condition",
  );
  if (ether.contributes) {
    delete ether.contributes.rankingSteps;
    delete ether.contributes.ruleConditions;
  }
}

const server = await serve(helloPlugin);
if (server.mode === "http") {
  console.log(`Hello plugin listening at ${server.url}`);
}
