import type { Profile } from "./profile.ts";

export const profile: Profile = {
  displays: {
    g9: { width: 3440 },
    aw: { width: 2560 },
    laptop: { width: 1512 },
  },
  windows: {
    browser: { app: "Browser" },
    "terminal-work": { app: "Terminal", title: "work" },
    "terminal-scratch": { app: "Terminal", title: "scratch" },
    editor: { app: "Editor" },
    notes: { app: "Notes" },
    calendar: { app: "Calendar" },
    music: { app: "Music" },
    chat: { app: "Chat" },
  },
  desk: [
    {
      display: "g9",
      label: "main",
      kind: "columns",
      tracks: [["browser", "notes"], ["terminal-work"], ["editor", "calendar"]],
    },
    {
      display: "laptop",
      label: "laptop",
      kind: "stack",
      tracks: [["browser", "terminal-work", "chat", "music"]],
    },
  ],
  weights: { columns: { "3": [3, 4, 3] } },
  deskSlots: [
    { name: "terminal-work" },
    { name: "browser", onDisplay: "g9" },
    { name: "editor" },
    { name: "notes" },
    { name: "calendar" },
    { name: "music" },
  ],
  laptopPinned: ["browser", "terminal-work", "editor", "notes", "browser"],
  laptopStackApps: ["Chat", "Music", "Calendar"],
};
