/**
 * The fixture gallery document: one plugin page that uses every ViewSpec
 * block, every cell kind, every Ether widget and every Ether frame, with the
 * loader data a plugin server would have computed. The gallery test renders
 * it so the owner can review how plugin pages look; the render tests assert
 * each piece lands. All copy here is plugin copy (already resolved by the
 * fixture "server"), so it renders as data, never through t().
 */
import type { PageSpec } from "@braedonsaunders/appkit-viewspec";

const titles = [
  {
    key: "1000000000000550",
    type: "movie",
    title: "The Example",
    year: 2024,
    rating: 8.4,
    summary: "A fixture title.",
  },
  {
    key: "2000000000001399",
    type: "show",
    title: "Example: The Series",
    year: 2021,
    rating: 9.1,
  },
  {
    key: "1000000000000603",
    type: "movie",
    title: "Another Example",
    year: 1999,
    rating: 7.2,
  },
];

export const galleryData = {
  pageTitle: "Gallery plugin",
  pageHint: "Every block and widget, rendered by Ether.",
  modeA: true,
  modeB: false,
  showExtra: true,
  hideMe: false,
  totalSpent: "$1,240.00",
  rows: [
    {
      id: "r1",
      code: "A-1",
      name: "First row",
      note: "owner@example.org",
      amount: "$42.00",
      count: "3",
      day: "Nov 1, 2026",
      state: "Ready",
      stateVariant: "success",
      href: "/catalog",
      recordId: "1000000000000550",
      drill: { href: "/catalog" },
      mark: "New",
    },
    {
      id: "r2",
      code: "B-2",
      amount: "$7.50",
      count: "1",
      day: "Nov 2, 2026",
      state: "Waiting",
      stateVariant: "warning",
      href: "https://example.org/row",
      recordId: "2000000000001399",
      drill: {},
      mark: "Hot",
    },
  ],
  totals: { amount: "$49.50" },
  total: 42,
  pageNum: 2,
  perPage: 10,
  sortKey: "name",
  sortDir: "asc",
  titles,
  featured: titles[0],
  health: [
    { label: "Guide", level: "ok", value: "Fresh" },
    { label: "Streams", level: "warn", value: "2 slow" },
    { label: "Recorder", level: "off" },
  ],
  channels: [
    {
      id: "c1",
      name: "Example One",
      number: 101,
      group: "News",
      nowPlaying: "Morning Example",
      level: "ok",
    },
    {
      id: "c2",
      name: "Example Two",
      number: 102,
      group: "Movies",
      nowPlaying: "The Example",
      level: "warn",
    },
  ],
  chartSeries: [
    { key: "plays", label: "Plays", values: [1, 3, 2, 5, 4, 6] },
    { key: "errors", label: "Errors", values: [0, 1, 0, 0, 2, 0] },
  ],
  chartTimes: [
    "2026-11-01T10:00:00Z",
    "2026-11-01T10:01:00Z",
    "2026-11-01T10:02:00Z",
    "2026-11-01T10:03:00Z",
    "2026-11-01T10:04:00Z",
    "2026-11-01T10:05:00Z",
  ],
};

const cell = (c: unknown) => c as never;

export const gallerySpec: PageSpec = {
  specVersion: 1,
  layout: "list",
  header: [
    {
      kind: "page-header",
      title: { $: "pageTitle" },
      description: { $: "pageHint" },
      actions: [
        { widget: "ether.badge", props: { label: "Fixture", variant: "outline" } },
        {
          widget: "ether.actionButton",
          props: { label: "Ping the plugin", action: "ping", variant: "outline" },
        },
      ],
    },
  ],
  body: [
    {
      kind: "filter-bar",
      controls: { search: true },
      searchPlaceholder: "Filter the gallery…",
      leading: {
        kind: "toggle-links",
        divider: true,
        links: [
          { href: "?mode=a", label: "Mode A", activeWhen: { $: "modeA" } },
          { href: "?mode=b", label: "Mode B", activeWhen: { $: "modeB" } },
        ],
      },
    },
    {
      kind: "summary-line",
      label: "Total spent",
      value: cell({ kind: "money", field: { $: "totalSpent" } }),
    },
    {
      kind: "grid",
      blocks: [
        {
          kind: "stat-tile",
          iconKey: "film",
          accent: "teal",
          label: "Titles",
          value: "1,204",
          sub: "Across 3 shelves",
        },
        {
          kind: "stat-tile",
          iconKey: "play",
          accent: "teal",
          label: "Plays today",
          value: "86",
          tone: "positive",
        },
        {
          kind: "stat-tile",
          iconKey: "alert",
          accent: "amber",
          label: "Needs a look",
          value: "2",
          tone: "warning",
        },
      ],
    },
    { kind: "widget", widget: "ether.healthStrip", props: { items: { $: "health" } } },
    {
      kind: "frame",
      frame: "ether.tabs",
      props: {
        tabs: [
          { key: "rows", label: "Rows" },
          { key: "about", label: "About" },
        ],
      },
      blocks: [
        { kind: "heading", level: 2, content: "Every cell kind" },
        { kind: "text", content: "The table below binds one column per cell renderer." },
        {
          kind: "table",
          variant: "app",
          rows: { $: "rows" },
          rowKey: { $: "id" },
          sorting: {
            basePath: "/p/gallery/home",
            sort: { $: "sortKey" },
            dir: { $: "sortDir" },
          },
          trailing: [
            {
              label: "Total",
              labelColSpan: 1,
              cells: [{ cell: cell({ kind: "money", field: { $: "totals.amount" } }) }],
            },
          ],
          columns: [
            {
              header: "Name",
              sort: "name",
              cell: cell({
                kind: "text",
                field: { $: "name" },
                prefix: { field: { $: "code" } },
                suffix: { field: { $: "note" } },
                fallback: "Untitled",
              }),
            },
            {
              header: "Amount",
              align: "right",
              cell: cell({ kind: "money", field: { $: "amount" } }),
            },
            {
              header: "Count",
              align: "right",
              cell: cell({ kind: "number", field: { $: "count" } }),
            },
            { header: "Day", cell: cell({ kind: "date", field: { $: "day" } }) },
            {
              header: "State",
              cell: cell({ kind: "badge", field: { $: "state" }, variant: { $: "stateVariant" } }),
            },
            {
              header: "Open",
              cell: cell({ kind: "link", field: { $: "code" }, href: { $: "href" } }),
            },
            {
              header: "Record",
              cell: cell({
                kind: "record-link",
                field: { $: "code" },
                recordType: "title",
                id: { $: "recordId" },
              }),
            },
            {
              header: "Drill",
              cell: cell({
                kind: "drill",
                target: { $: "drill" },
                inner: { kind: "text", field: { $: "amount" } },
              }),
            },
            {
              header: "Entry",
              cell: cell({
                kind: "txn",
                target: { $: "drill" },
                inner: { kind: "badge", field: { $: "state" } },
              }),
            },
            {
              header: "Mark",
              cell: cell({
                kind: "widget",
                widget: "ether.badge",
                props: { label: { $: "mark" } },
              }),
            },
          ],
        },
        {
          kind: "pagination",
          basePath: "/p/gallery/home",
          total: { $: "total" },
          page: { $: "pageNum" },
          perPage: { $: "perPage" },
        },
      ],
    },
    {
      kind: "paper",
      title: "Paper statement",
      company: "Gallery Co",
      periodPhrase: "For November 2026",
      note: "Paper renders as a console card on plugin pages.",
      blocks: [{ kind: "text", content: "Statement body from the plugin loader." }],
    },
    {
      kind: "panel",
      title: "Poster shelf panel",
      hint: "A panel block around an Ether shelf widget.",
      blocks: [
        {
          kind: "widget",
          widget: "ether.posterShelf",
          props: { items: { $: "titles" }, label: "Gallery shelf" },
        },
      ],
    },
    { kind: "heading", level: 3, content: "Poster grid and featured title" },
    {
      kind: "widget",
      widget: "ether.posterGrid",
      props: { items: { $: "titles" } },
    },
    {
      kind: "widget",
      widget: "ether.titleCard",
      props: { item: { $: "featured" }, reason: "Featured by the fixture" },
    },
    {
      kind: "widget",
      widget: "ether.liveChart",
      props: {
        series: { $: "chartSeries" },
        times: { $: "chartTimes" },
        label: "Plays and errors",
        area: true,
      },
    },
    { kind: "heading", level: 3, content: "Channels" },
    {
      kind: "repeat",
      items: { $: "channels" },
      itemKey: { $: "id" },
      empty: { text: "No channels yet" },
      blocks: [
        {
          kind: "widget",
          widget: "ether.channelRow",
          props: {
            name: { $: "name" },
            number: { $: "number" },
            group: { $: "group" },
            nowPlaying: { $: "nowPlaying" },
            level: { $: "level" },
          },
        },
      ],
    },
    {
      kind: "frame",
      frame: "ether.section",
      props: {
        title: "Plugin settings",
        description: "A settings sub-schema, saved through an action.",
      },
      blocks: [
        {
          kind: "widget",
          widget: "ether.settingsForm",
          props: {
            action: "save-settings",
            submitLabel: "Save settings",
            values: { region: "us", refreshHours: 6, enabled: true },
            groups: [
              {
                title: "General",
                fields: [
                  {
                    key: "region",
                    label: "Region",
                    kind: "select",
                    options: [
                      { value: "us", label: "United States" },
                      { value: "ca", label: "Canada" },
                    ],
                  },
                  { key: "refreshHours", label: "Refresh every (hours)", kind: "number" },
                  { key: "enabled", label: "Enabled", kind: "bool" },
                ],
              },
            ],
          },
        },
        {
          kind: "widget",
          widget: "ether.titleMatch",
          props: {
            action: "match-title",
            label: "Match the unmatched folder",
            params: { folder: "movies/Untitled (2024)" },
          },
        },
      ],
    },
    {
      kind: "frame",
      frame: "ether.drawer",
      props: { title: "Drawer details", openLabel: "Open the drawer" },
      blocks: [
        { kind: "text", content: "Drawer body from the plugin loader." },
        { kind: "widget", widget: "ether.badge", props: { label: "Inside" } },
      ],
    },
    { kind: "text", content: "Always shown", when: { $: "showExtra" } },
    { kind: "text", content: "ShouldNotAppear", when: { $: "hideMe" } },
  ],
};

export const galleryDoc = {
  spec: gallerySpec,
  data: galleryData,
  title: "Gallery plugin",
};
