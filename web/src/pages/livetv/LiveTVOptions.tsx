import { Button, Input } from "@braedonsaunders/appkit-ui";
import { useEffect, useState } from "react";
import { Drawer } from "../../components/Drawer";
import { Field } from "../../components/fields";
import { t } from "../../i18n";
import { api, errorMessage } from "../../lib/api";
import type { EtherConfig } from "../../lib/types";

export function LiveTVOptions() {
  const [open, setOpen] = useState(false);
  const [url, setUrl] = useState("");
  const [minutes, setMinutes] = useState(0);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  useEffect(() => {
    if (!open) return;
    setLoading(true);
    api
      .getConfig()
      .then((cfg) => {
        setUrl(cfg.liveTV?.logoIndexUrl ?? "");
        setMinutes(cfg.liveTV?.healthSweepMinutes ?? 0);
        setError("");
      })
      .catch((e) => setError(errorMessage(e)))
      .finally(() => setLoading(false));
  }, [open]);
  const save = async () => {
    setLoading(true);
    setError("");
    try {
      const cfg = await api.getConfig();
      await api.putConfig({
        ...cfg,
        liveTV: { ...cfg.liveTV, logoIndexUrl: url, healthSweepMinutes: minutes },
      } as EtherConfig);
      setOpen(false);
    } catch (e) {
      setError(errorMessage(e));
    } finally {
      setLoading(false);
    }
  };
  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)}>
        {t("Channel options")}
      </Button>
      <Drawer open={open} onClose={() => setOpen(false)} title={t("Channel options")}>
        <div className="space-y-4">
          <Field
            htmlFor="livetv-logo-index"
            label={t("Logo index URL")}
            hint={t(
              "Optional JSON index supplied by you. Channels without a matching logo show their name.",
            )}
          >
            <Input id="livetv-logo-index" type="url" value={url} onChange={(e) => setUrl(e.currentTarget.value)} />
          </Field>
          <Field
            htmlFor="livetv-health-interval"
            label={t("Health check interval in minutes")}
            hint={t("Use 0 for the default interval or -1 to turn checks off.")}
          >
            <Input
              id="livetv-health-interval"
              type="number"
              min={-1}
              value={minutes}
              onChange={(e) => setMinutes(Number(e.currentTarget.value))}
            />
          </Field>
          {error ? (
            <p role="alert" className="text-sm text-danger">
              {error}
            </p>
          ) : null}
          <Button disabled={loading} onClick={() => void save()}>
            {t("Save")}
          </Button>
        </div>
      </Drawer>
    </>
  );
}
