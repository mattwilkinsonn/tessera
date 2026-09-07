// Runtime profile-loader contract (O1). `loadProfile` resolves the profile the
// binary runs on: `$TESSERA_PROFILE` override → `~/.config/tessera/profile.ts`
// well-known path → the bundled default. These tests are hermetic — every path
// they touch lives under a temp dir they create and clean, never live
// `~/.config`.

import { afterEach, describe, expect, test } from "bun:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { profile as defaultProfile } from "./config/profile.ts";
import { loadProfile } from "./index.ts";

// A minimal alternate profile whose displays are distinct from the bundled
// default, so a successful load is observable by identity of the data.
const ALT_PROFILE = `import type { Profile } from "${join(import.meta.dir, "config/types.ts")}";
export const profile = {
	displays: { g9: { width: 111 }, aw: { width: 222 }, laptop: { width: 333 } },
	windows: { solo: { app: /Solo/ } },
	desk: [{ display: "laptop", label: "laptop", kind: "stack", columns: [["solo"]] }],
	ratios: { col3Root: 0.3, col3Inner: 0.5714 },
	deskSlots: [{ name: "solo" }],
	laptopPinned: ["solo"],
	laptopStackApps: {},
} satisfies Profile;
`;

describe("loadProfile", () => {
	let root = "";
	const savedEnv = {
		TESSERA_PROFILE: process.env.TESSERA_PROFILE,
		XDG_CONFIG_HOME: process.env.XDG_CONFIG_HOME,
	};

	afterEach(() => {
		if (root !== "") {
			rmSync(root, { recursive: true, force: true });
			root = "";
		}
		process.env.TESSERA_PROFILE = savedEnv.TESSERA_PROFILE;
		process.env.XDG_CONFIG_HOME = savedEnv.XDG_CONFIG_HOME;
	});

	test("$TESSERA_PROFILE override loads the named file", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-loader-"));
		const alt = join(root, "alt.ts");
		writeFileSync(alt, ALT_PROFILE);
		process.env.TESSERA_PROFILE = alt;

		const loaded = await loadProfile();
		expect(loaded.displays).toEqual({
			g9: { width: 111 },
			aw: { width: 222 },
			laptop: { width: 333 },
		});
	});

	test("missing well-known file falls back to the bundled default", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-loader-"));
		// XDG points at a config root with NO tessera/profile.ts present.
		process.env.TESSERA_PROFILE = undefined;
		delete process.env.TESSERA_PROFILE;
		process.env.XDG_CONFIG_HOME = join(root, "config");

		const loaded = await loadProfile();
		expect(loaded).toBe(defaultProfile);
	});

	test("well-known ~/.config/tessera/profile.ts is loaded when present", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-loader-"));
		delete process.env.TESSERA_PROFILE;
		const configHome = join(root, "config");
		mkdirSync(join(configHome, "tessera"), { recursive: true });
		writeFileSync(join(configHome, "tessera", "profile.ts"), ALT_PROFILE);
		process.env.XDG_CONFIG_HOME = configHome;

		const loaded = await loadProfile();
		expect(loaded.displays.g9.width).toBe(111);
	});

	// The load-bearing O1 contract: an explicit override or a present-but-broken
	// well-known file MUST surface its error, never silently fall back to the
	// default. These guard against a future "robustness" refactor that wraps the
	// dynamic import in a swallowing try/catch (which the happy-path tests above
	// would not catch).
	test("a missing $TESSERA_PROFILE override surfaces the error", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-loader-"));
		process.env.TESSERA_PROFILE = join(root, "does-not-exist.ts");

		await expect(loadProfile()).rejects.toThrow();
	});

	test("a broken $TESSERA_PROFILE override surfaces the error", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-loader-"));
		const broken = join(root, "broken.ts");
		writeFileSync(broken, "this is not valid typescript {{{");
		process.env.TESSERA_PROFILE = broken;

		await expect(loadProfile()).rejects.toThrow();
	});

	test("a broken well-known profile.ts surfaces the error", async () => {
		root = mkdtempSync(join(tmpdir(), "tess-loader-"));
		delete process.env.TESSERA_PROFILE;
		const configHome = join(root, "config");
		mkdirSync(join(configHome, "tessera"), { recursive: true });
		writeFileSync(
			join(configHome, "tessera", "profile.ts"),
			"not valid ts {{{",
		);
		process.env.XDG_CONFIG_HOME = configHome;

		await expect(loadProfile()).rejects.toThrow();
	});
});
