package catalog

import "github.com/braedonsaunders/ether/internal/store"

func init() {
	store.Register("catalog", 29, `
CREATE TRIGGER catalog_show_added_insert AFTER INSERT ON catalog_titles
WHEN new.type IN ('show','episode') BEGIN
 UPDATE catalog_titles SET show_added_ts=COALESCE((SELECT e.released_ts FROM catalog_titles e WHERE e.type='episode' AND e.show_key=catalog_titles.key AND e.release_known=1 AND e.released_ts<=CAST(strftime('%s','now') AS INTEGER) ORDER BY e.released_ts DESC LIMIT 1), CASE WHEN released_ts<>0 THEN released_ts ELSE 0 END)
 WHERE type='show' AND key=CASE WHEN new.type='show' THEN new.key ELSE new.show_key END;
END;
CREATE TRIGGER catalog_show_added_delete AFTER DELETE ON catalog_titles
WHEN old.type='episode' BEGIN
 UPDATE catalog_titles SET show_added_ts=COALESCE((SELECT e.released_ts FROM catalog_titles e WHERE e.type='episode' AND e.show_key=catalog_titles.key AND e.release_known=1 AND e.released_ts<=CAST(strftime('%s','now') AS INTEGER) ORDER BY e.released_ts DESC LIMIT 1), CASE WHEN released_ts<>0 THEN released_ts ELSE 0 END)
 WHERE type='show' AND key=old.show_key;
END;
CREATE TRIGGER catalog_show_added_update AFTER UPDATE OF type,show_key,released_ts ON catalog_titles BEGIN
 UPDATE catalog_titles SET show_added_ts=COALESCE((SELECT e.released_ts FROM catalog_titles e WHERE e.type='episode' AND e.show_key=catalog_titles.key AND e.release_known=1 AND e.released_ts<=CAST(strftime('%s','now') AS INTEGER) ORDER BY e.released_ts DESC LIMIT 1), CASE WHEN released_ts<>0 THEN released_ts ELSE 0 END)
 WHERE type='show' AND (key=CASE WHEN old.type='show' THEN old.key WHEN old.type='episode' THEN old.show_key ELSE 0 END OR key=CASE WHEN new.type='show' THEN new.key WHEN new.type='episode' THEN new.show_key ELSE 0 END);
END;
`)
	store.RegisterDialect("catalog", 29, store.Postgres, `
CREATE FUNCTION catalog_refresh_show_added(k BIGINT) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM catalog_titles WHERE key=k AND type='show' FOR UPDATE;
 UPDATE catalog_titles t SET show_added_ts=COALESCE((SELECT e.released_ts FROM catalog_titles e WHERE e.type='episode' AND e.show_key=t.key AND e.release_known=1 AND e.released_ts<=EXTRACT(EPOCH FROM CURRENT_TIMESTAMP) ORDER BY e.released_ts DESC LIMIT 1), CASE WHEN t.released_ts<>0 THEN t.released_ts ELSE 0 END) WHERE t.type='show' AND t.key=k;
END $$;
CREATE FUNCTION catalog_show_added_change() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE oldkey BIGINT; newkey BIGINT;
BEGIN
 IF TG_OP<>'INSERT' THEN oldkey := CASE WHEN OLD.type='show' THEN OLD.key WHEN OLD.type='episode' THEN OLD.show_key END; END IF;
 IF TG_OP<>'DELETE' THEN newkey := CASE WHEN NEW.type='show' THEN NEW.key WHEN NEW.type='episode' THEN NEW.show_key END; END IF;
 IF oldkey IS NOT NULL AND newkey IS NOT NULL AND oldkey>newkey THEN PERFORM catalog_refresh_show_added(newkey); PERFORM catalog_refresh_show_added(oldkey);
 ELSE IF oldkey IS NOT NULL THEN PERFORM catalog_refresh_show_added(oldkey); END IF; IF newkey IS NOT NULL AND newkey IS DISTINCT FROM oldkey THEN PERFORM catalog_refresh_show_added(newkey); END IF; END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER catalog_show_added_change AFTER INSERT OR DELETE OR UPDATE OF type,show_key,released_ts ON catalog_titles FOR EACH ROW EXECUTE FUNCTION catalog_show_added_change();
`)
}
