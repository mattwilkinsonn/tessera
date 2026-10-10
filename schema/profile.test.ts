import { describe, expect, test } from "bun:test";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { profile as defaultProfile } from "./default.ts";
import { profile as fullProfile } from "./full.ts";
import { Profile } from "./profile.ts";

const defaultPath = join(import.meta.dir, "../internal/config/default.json");
const fullPath = join(import.meta.dir, "../internal/config/testdata/full.json");

function rendered(value: unknown): string {
  return `${JSON.stringify(Profile.parse(value), null, 2)}\n`;
}

function checkOrUpdate(path: string, value: unknown): void {
  const expected = rendered(value);
  if (process.env.UPDATE === "1") {
    mkdirSync(dirname(path), { recursive: true });
    writeFileSync(path, expected);
    return;
  }
  expect(readFileSync(path, "utf8")).toBe(expected);
}

describe("Profile schema", () => {
  test("strict objects reject unknown keys in callback literals", () => {
    const layouts = ["g9"].map((display) => ({
      display,
      label: "main",
      kind: "columns",
      tracks: [["browser"], ["editor"]],
      wieghts: [1, 1],
    }));
    expect(() => Profile.parse({ ...defaultProfile, desk: layouts })).toThrow();
  });

  test("stack layouts accept exactly one track and no weights", () => {
    const layout = {
      display: "g9",
      label: "main",
      kind: "stack",
      tracks: [["browser"]],
    };
    expect(() => Profile.parse({ ...defaultProfile, desk: [layout] })).not.toThrow();
    expect(() => Profile.parse({
      ...defaultProfile,
      desk: [{ ...layout, tracks: [["browser"], ["editor"]] }],
    })).toThrow();
    expect(() => Profile.parse({
      ...defaultProfile,
      desk: [{ ...layout, weights: [1] }],
    })).toThrow();
  });

  test("unknown layout kinds are rejected", () => {
    const layout = {
      display: "g9",
      label: "main",
      kind: "grid",
      tracks: [["browser"]],
    };
    expect(() => Profile.parse({ ...defaultProfile, desk: [layout] })).toThrow();
  });

  test("default and full JSON stay in sync with their TS sources", () => {
    checkOrUpdate(defaultPath, defaultProfile);
    checkOrUpdate(fullPath, fullProfile);
  });
});

