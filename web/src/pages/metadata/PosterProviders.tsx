import { Button, toast } from "@braedonsaunders/appkit-ui";
import { useState } from "react";
import { MutationError } from "../../components/feedback";
import { t } from "../../i18n";
import { api, apiFetch } from "../../lib/api";
import { useAsync } from "../../lib/hooks";
import { normalizePosters, posterPayload, POSTER_SERVICES } from "../settings/PosterAccounts";
import { nativeSelect } from "./shared";
export function PosterProviders({ onSaved }: { onSaved: () => void }) {
  const config = useAsync(async () => normalizePosters((await api.getConfig()).posters), []);
  const [error, setError] = useState<unknown>(null);
  const [saving, setSaving] = useState(false);
  return (
    <section className="metadata-control-section">
      <div className="metadata-section-title">
        <h3>{t("Poster source")}</h3>
      </div>
      <select
        aria-label={t("Poster source")}
        className={nativeSelect}
        disabled={!config.data || saving}
        value={config.data?.source ?? "tmdb"}
        onChange={async (e) => {
          setSaving(true);
          setError(null);
          try {
            const result = await apiFetch<{ value: NonNullable<typeof config.data> }>(
              "/config/posters",
              {
                method: "PUT",
                body: JSON.stringify(posterPayload({ ...config.data!, source: e.target.value })),
              },
            );
            config.setData(result.value);
            onSaved();
            toast.success(t("Poster provider saved"));
          } catch (e) {
            setError(e);
          } finally {
            setSaving(false);
          }
        }}
      >
        <option value="tmdb">TMDB</option>
        {POSTER_SERVICES.filter((s) => config.data?.accounts?.[s.id]?.enabled).map((s) => (
          <option key={s.id} value={s.id}>
            {s.name}
          </option>
        ))}
      </select>
      {error || config.error ? (
        <>
          <MutationError error={error || config.error} />
          <Button onClick={() => void config.reload()}>{t("Retry")}</Button>
        </>
      ) : null}
    </section>
  );
}
