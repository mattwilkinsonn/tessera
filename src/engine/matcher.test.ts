// matcher (T2): the single-engine window-match predicate + slug resolver, plus
// the WIN re-validation acceptance test — does each ported profile RegExp
// classify a recorded live-window fixture identically to the documented bash
// behavior (T2)?

import { describe, expect, test } from "bun:test";
import { profile } from "../config/profile.ts";
import type { Profile, WindowSpec } from "../config/types.ts";
import { matchesSpec, slugForWindow } from "./matcher.ts";

describe("matchesSpec", () => {
	test("app-only spec matches any title (arc /Arc/)", () => {
		const spec: WindowSpec = { app: /Arc/ };
		expect(matchesSpec(spec, "Arc", "some window")).toBe(true);
		expect(matchesSpec(spec, "Arc", "")).toBe(true);
		expect(matchesSpec(spec, "Ghostty", "Arc")).toBe(false);
	});

	test("empty title = match any", () => {
		const spec: WindowSpec = { app: /Ghostty/ };
		expect(matchesSpec(spec, "Ghostty", "pc | foo")).toBe(true);
		expect(matchesSpec(spec, "Ghostty", "mbp | bar")).toBe(true);
	});

	test("title-present must match (ghostty-wave /pc/)", () => {
		const spec: WindowSpec = { app: /Ghostty/, title: /pc/ };
		expect(matchesSpec(spec, "Ghostty", "pc | model-evals")).toBe(true);
		expect(matchesSpec(spec, "Ghostty", "mbp | mbp")).toBe(false);
	});

	test("titleInvert matches when title does NOT match", () => {
		const spec: WindowSpec = { app: /Ghostty/, title: /pc/, titleInvert: true };
		expect(matchesSpec(spec, "Ghostty", "mbp | bar")).toBe(true);
		expect(matchesSpec(spec, "Ghostty", "pc | foo")).toBe(false);
	});

	test("substring/unanchored semantics (/Code/ in an app name)", () => {
		const spec: WindowSpec = { app: /Code/ };
		expect(matchesSpec(spec, "Visual Studio Code", "")).toBe(true);
		expect(matchesSpec(spec, "Code", "")).toBe(true);
	});
});

describe("slugForWindow", () => {
	test("returns the sorted-first matching KEY, title disambiguates", () => {
		// Both ghostty specs share app /Ghostty/; the title drives the key.
		expect(slugForWindow(profile, "Ghostty", "pc | model-evals")).toBe(
			"ghostty-wave",
		);
		expect(slugForWindow(profile, "Ghostty", "mbp | mbp")).toBe("ghostty-mbp");
	});

	test("sorted-key precedence breaks a genuine spec collision", () => {
		// The current profile has no two specs that match one app+title, so sort
		// order is untestable-by-construction against `profile`. Build a synthetic
		// profile with two colliding app-only specs to lock the tie-break: keys
		// "alpha" and "omega" both match "X"; sorted iteration must return the
		// lexicographically-first key ("alpha").
		const collide: Profile = {
			...profile,
			windows: { omega: { app: /X/ }, alpha: { app: /X/ } },
		};
		expect(slugForWindow(collide, "X", "")).toBe("alpha");
	});

	test("fallback squeeze for a non-profile app", () => {
		expect(slugForWindow(profile, "1Password", "")).toBe("1password");
		expect(slugForWindow(profile, "Activity Monitor", "")).toBe(
			"activity-monitor",
		);
		expect(slugForWindow(profile, "System Settings", "")).toBe(
			"system-settings",
		);
		// Edge cases the squeeze explicitly handles (tr -cs 'a-z0-9' '-' + strip):
		// an all-punctuation app squeezes+strips to empty;
		expect(slugForWindow(profile, "!!!", "")).toBe("");
		// a multi-space run squeezes to ONE hyphen (tr -s semantics);
		expect(slugForWindow(profile, "Foo   Bar", "")).toBe("foo-bar");
		// leading/trailing separators are stripped (${s#-} / ${s%-}).
		expect(slugForWindow(profile, "  Baz  ", "")).toBe("baz");
	});
});

// ── WIN re-validation (design T2 acceptance) ────────────────────────────
// A raw `yabai -m query --windows` capture: snake_case fields, NOT the
// camelCase WmWindow shape. Extract only `app` + `title` and assert each ported
// profile RegExp classifies these real windows as documented.
interface RawWindow {
	app: string;
	title: string;
}

const rawWindows: RawWindow[] = (await Bun.file(
	`${import.meta.dir}/fixtures/windows.live.json`,
).json()) as RawWindow[];

function findWindow(pred: (w: RawWindow) => boolean): RawWindow {
	const w = rawWindows.find(pred);
	expect(w).toBeDefined();
	if (w == null) {
		throw new Error("fixture window not found");
	}
	return w;
}

describe("WIN re-validation against live fixture (T2 acceptance)", () => {
	test("every profile spec's app matcher is a RegExp", () => {
		for (const [, spec] of Object.entries(profile.windows)) {
			expect(spec.app).toBeInstanceOf(RegExp);
		}
	});

	test("the two Ghostty windows disambiguate by title (D10 latent-bug fix)", () => {
		const waveSpec = profile.windows["ghostty-wave"];
		const mbpSpec = profile.windows["ghostty-mbp"];
		expect(waveSpec).toBeDefined();
		expect(mbpSpec).toBeDefined();
		if (waveSpec == null || mbpSpec == null) {
			throw new Error("ghostty specs missing");
		}
		const pc = findWindow(
			(w) => w.app === "Ghostty" && w.title.startsWith("pc"),
		);
		const mbp = findWindow(
			(w) => w.app === "Ghostty" && w.title.startsWith("mbp"),
		);

		expect(matchesSpec(waveSpec, pc.app, pc.title)).toBe(true);
		expect(matchesSpec(mbpSpec, pc.app, pc.title)).toBe(false);
		expect(matchesSpec(mbpSpec, mbp.app, mbp.title)).toBe(true);
		expect(matchesSpec(waveSpec, mbp.app, mbp.title)).toBe(false);

		expect(slugForWindow(profile, pc.app, pc.title)).toBe("ghostty-wave");
		expect(slugForWindow(profile, mbp.app, mbp.title)).toBe("ghostty-mbp");
	});

	test("every Arc window matches the arc spec", () => {
		const arcSpec = profile.windows.arc;
		expect(arcSpec).toBeDefined();
		if (arcSpec == null) {
			throw new Error("arc spec missing");
		}
		const arcs = rawWindows.filter((w) => w.app === "Arc");
		expect(arcs.length).toBe(6);
		for (const w of arcs) {
			expect(matchesSpec(arcSpec, w.app, w.title)).toBe(true);
		}
	});

	test("a Code window matches the vscode spec", () => {
		const vscodeSpec = profile.windows.vscode;
		expect(vscodeSpec).toBeDefined();
		if (vscodeSpec == null) {
			throw new Error("vscode spec missing");
		}
		const code = findWindow((w) => w.app === "Code");
		expect(matchesSpec(vscodeSpec, code.app, code.title)).toBe(true);
	});

	test("non-profile apps match NONE of the profile specs", () => {
		const specs = Object.values(profile.windows);
		for (const app of ["1Password", "Ferdium", "Messages", "Notion"]) {
			const w = findWindow((x) => x.app === app);
			for (const spec of specs) {
				expect(matchesSpec(spec, w.app, w.title)).toBe(false);
			}
		}
	});

	test("slugForWindow on a non-profile app returns the squeezed fallback", () => {
		const w = findWindow((x) => x.app === "1Password");
		expect(slugForWindow(profile, w.app, w.title)).toBe("1password");
	});
});
