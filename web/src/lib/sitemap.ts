/**
 * Every owner destination in one place, derived from the descriptor
 * registry's page descriptors (internal/app/extend_console.go). The sidebar
 * and ⌘K search read this, so a section only ever has one home.
 */

import { t } from "../i18n";
import { textOf, type Descriptor } from "./extend";
import { pageRoute, searchTextOf, type PageMeta } from "./console-descriptors";

export interface Destination {
  href: string;
  title: string;
  /** Page the destination belongs to, shown as the search subtitle. */
  page: string;
  keywords?: string;
  /** A whole page rather than a section within one. */
  isPage?: boolean;
}

export interface PageDef {
  href: string;
  title: string;
  /** In-page sections (tabs) in display order. */
  sections: { key: string; title: string; keywords?: string }[];
  keywords?: string;
}

/** Pages in registry order, with their sections, in the active language. */
export function pagesFrom(pages: Descriptor<PageMeta>[]): PageDef[] {
  return pages.map((p) => ({
    href: pageRoute(p),
    title: textOf(p.label),
    sections: (p.meta?.sections ?? []).map((s) => ({
      key: s.key,
      title: textOf(s.label),
      keywords: searchTextOf(s.keywords),
    })),
    keywords: searchTextOf(p.meta?.keywords),
  }));
}

export function destinationsFrom(pages: Descriptor<PageMeta>[]): Destination[] {
  return pagesFrom(pages).flatMap((p) => [
    { href: p.href, title: p.title, page: t("Page"), keywords: p.keywords, isPage: true },
    ...p.sections.map((s) => ({
      href: `${p.href}/${s.key}`,
      title: s.title,
      page: p.title,
      keywords: s.keywords,
    })),
  ]);
}
