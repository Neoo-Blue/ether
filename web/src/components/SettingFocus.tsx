import { useEffect } from "react";
import { useLocation, useNavigate } from "react-router-dom";

const PREFIX = "#setting=";

/** A link that opens a page and points at one setting on it. */
export function settingHref(href: string, title: string, more?: string): string {
  const q = new URLSearchParams({ t: title });
  if (more) q.set("m", more);
  return `${href}${PREFIX}${q.toString()}`;
}

function findRow(root: ParentNode, title: string): HTMLElement | null {
  const want = title.trim();
  let found: HTMLElement | null = null;
  root.querySelectorAll<HTMLElement>("label, div, span, p").forEach((el) => {
    if (found || el.textContent?.trim() !== want) return;
    // The innermost element carrying exactly this text is the row's title.
    const inner = Array.from(el.children).some((c) => c.textContent?.trim() === want);
    if (!inner) found = el;
  });
  if (!found) return null;
  const el = found as HTMLElement;
  return (el.closest(".px-5") as HTMLElement | null) ?? el.parentElement ?? el;
}

/**
 * Follows a ⌘K jump to one setting: waits for the page to render, opens the
 * "More options" disclosure that hides it, then scrolls to and highlights it.
 */
export function SettingFocus() {
  const location = useLocation();
  const navigate = useNavigate();
  useEffect(() => {
    if (!location.hash.startsWith(PREFIX)) return;
    const q = new URLSearchParams(location.hash.slice(PREFIX.length));
    const title = q.get("t") ?? "";
    const more = q.get("m");
    let tries = 0;
    let opened = false;
    const timer = window.setInterval(() => {
      tries++;
      const root = document.querySelector("main") ?? document.body;
      const row = findRow(root, title);
      if (row) {
        window.clearInterval(timer);
        row.scrollIntoView({ block: "center", behavior: "smooth" });
        row.classList.add("setting-focus");
        window.setTimeout(() => row.classList.remove("setting-focus"), 2400);
        navigate(`${location.pathname}${location.search}`, { replace: true });
        return;
      }
      if (more && !opened) {
        const button = Array.from(
          root.querySelectorAll<HTMLButtonElement>('button[aria-expanded="false"]'),
        ).find((b) => b.getAttribute("aria-label") === more);
        if (button) {
          button.click();
          opened = true;
        }
      }
      if (tries > 40) {
        window.clearInterval(timer);
        navigate(`${location.pathname}${location.search}`, { replace: true });
      }
    }, 100);
    return () => window.clearInterval(timer);
  }, [location.hash, location.pathname, location.search, navigate]);
  return null;
}
