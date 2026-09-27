// Runtime profile-loader contract. Tests use only temporary profile paths.
import { afterEach, describe, expect, test } from "bun:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { profile as defaultProfile } from "./config/profile.ts";
import { loadProfile } from "./index.ts";

const ALT_PROFILE = `import type { Profile } from "${join(import.meta.dir, "config/types.ts")}";
export const profile = {
	displays: { g9: { width: 111 }, aw: { width: 222 }, laptop: { width: 333 } },
	windows: { solo: { app: /Solo/ } },
	desk: [{ display: "laptop", label: "laptop", kind: "stack", tracks: [["solo"]] }],
	deskSlots: [{ name: "solo" }],
	laptopPinned: ["solo"],
	laptopStackApps: {},
} satisfies Profile;
`;

describe("loadProfile", () => {
	let root = "";
	const savedProfile = process.env.TESSERA_PROFILE;
	const savedXdg = process.env.XDG_CONFIG_HOME;
	afterEach(() => {
		if (savedProfile == null) delete process.env.TESSERA_PROFILE;
		else process.env.TESSERA_PROFILE = savedProfile;
		if (savedXdg == null) delete process.env.XDG_CONFIG_HOME;
		else process.env.XDG_CONFIG_HOME = savedXdg;
		if (root !== "") rmSync(root, { recursive: true, force: true });
		root = "";
	});

	test("loads an explicit profile override", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-loader-"));
		const path = join(root, "alt.ts");
		writeFileSync(path, ALT_PROFILE);
		process.env.TESSERA_PROFILE = path;
		expect((await loadProfile()).displays.laptop.width).toBe(333);
	});

	test("missing well-known profile falls back to the bundled default", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-loader-"));
		delete process.env.TESSERA_PROFILE;
		process.env.XDG_CONFIG_HOME = join(root, "config");
		expect(await loadProfile()).toBe(defaultProfile);
	});

	test("loads the well-known profile when present", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-loader-"));
		delete process.env.TESSERA_PROFILE;
		const configHome = join(root, "config");
		mkdirSync(join(configHome, "tessera"), { recursive: true });
		writeFileSync(join(configHome, "tessera", "profile.ts"), ALT_PROFILE);
		process.env.XDG_CONFIG_HOME = configHome;
		expect((await loadProfile()).displays.g9.width).toBe(111);
	});

	test("a broken well-known profile surfaces the error", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-loader-"));
		const configHome = join(root, "config");
		const directory = join(configHome, "tessera");
		mkdirSync(directory, { recursive: true });
		writeFileSync(join(directory, "profile.ts"), "export const profile = {");
		process.env.XDG_CONFIG_HOME = configHome;
		await expect(loadProfile()).rejects.toThrow();
	});

	test("an override with an incomplete topology is rejected", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-loader-"));
		const invalid = join(root, "invalid.ts");
		const profileWithTopology = ALT_PROFILE.replace(
			'desk: [{ display: "laptop", label: "laptop", kind: "stack", tracks: [["solo"]] }],',
			'topologies: [{ name: "aw-laptop", displays: ["aw", "laptop"], desk: [{ display: "laptop", label: "laptop", kind: "stack", tracks: [["solo"]] }] }],\n\tdesk: [{ display: "laptop", label: "laptop", kind: "stack", tracks: [["solo"]] }],',
		);
		writeFileSync(invalid, profileWithTopology);
		process.env.TESSERA_PROFILE = invalid;
		await expect(loadProfile()).rejects.toThrow("aw-laptop");
	});
});
