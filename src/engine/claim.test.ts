// ClaimSet (T2): the distinct-window claim engine — instance-scoped claimed
// set (the ported `_CLAIMED` global), display preference, dedup, and reset.

import { describe, expect, test } from "bun:test";
import { profile } from "../config/profile.ts";
import type { SpaceId, WmWindow } from "../driver/types.ts";
import { ClaimSet } from "./claim.ts";

const SPACE = "s1" as SpaceId;

function win(
	id: number,
	app: string,
	opts: {
		title?: string;
		displayIdx?: number;
		minimized?: boolean;
		floating?: boolean;
	} = {},
): WmWindow {
	return {
		id,
		app,
		title: opts.title ?? "",
		displayIdx: opts.displayIdx ?? 1,
		spaceId: SPACE,
		minimized: opts.minimized ?? false,
		floating: opts.floating ?? false,
		sticky: false,
		visible: true,
		splitType: "none",
		frame: { x: 0, y: 0, w: 100, h: 100 },
	};
}

describe("ClaimSet.claim (win_id_claim)", () => {
	test("pass 1 prefers a candidate on the target display", () => {
		const cs = new ClaimSet(profile);
		// First in array order is on display 2; the preferred (display 3) is later.
		const windows = [
			win(1, "Arc", { displayIdx: 2 }),
			win(2, "Arc", { displayIdx: 3 }),
		];
		expect(cs.claim(windows, "arc", 3)).toBe(2);
	});

	test("preferDisplay with no candidate on it falls through to pass 2", () => {
		const cs = new ClaimSet(profile);
		// Both candidates are on display 1; prefer display 9 (absent) → pass 2
		// picks the first unclaimed regardless of display.
		const windows = [
			win(1, "Arc", { displayIdx: 1 }),
			win(2, "Arc", { displayIdx: 1 }),
		];
		expect(cs.claim(windows, "arc", 9)).toBe(1);
	});

	test("preferred-display candidate already claimed falls through to pass 2", () => {
		const cs = new ClaimSet(profile);
		const windows = [
			win(1, "Arc", { displayIdx: 3 }),
			win(2, "Arc", { displayIdx: 5 }),
		];
		// First claim takes the display-3 window (the only one on preferred=3).
		expect(cs.claim(windows, "arc", 3)).toBe(1);
		// Second claim prefers display 3 again, but its only candidate is claimed
		// → pass 2 hands out the display-5 window.
		expect(cs.claim(windows, "arc", 3)).toBe(2);
	});

	test("distinct claims hand out different ids; a third returns null", () => {
		const cs = new ClaimSet(profile);
		const windows = [win(1, "Arc"), win(2, "Arc")];
		const a = cs.claim(windows, "arc");
		const b = cs.claim(windows, "arc");
		expect(a).not.toBe(b);
		expect([a, b].sort()).toEqual([1, 2]);
		expect(cs.claim(windows, "arc")).toBeNull();
	});

	test("no matching window returns null; absent name returns null", () => {
		const cs = new ClaimSet(profile);
		expect(cs.claim([win(1, "Ghostty", { title: "pc" })], "arc")).toBeNull();
		expect(cs.claim([win(1, "Arc")], "no-such-name")).toBeNull();
	});

	test("minimized / floating candidates are filtered out", () => {
		const cs = new ClaimSet(profile);
		const windows = [
			win(1, "Arc", { minimized: true }),
			win(2, "Arc", { floating: true }),
			win(3, "Arc"),
		];
		expect(cs.claim(windows, "arc")).toBe(3);
	});
});

describe("ClaimSet.claimMany (win_ids_claim)", () => {
	test("returns distinct ids in name order, skipping names with no free window", () => {
		const cs = new ClaimSet(profile);
		const windows = [
			win(1, "Arc"),
			win(2, "Ghostty", { title: "pc | x" }),
			win(3, "Spotify"),
		];
		// discord has no window → skipped.
		expect(
			cs.claimMany(windows, ["arc", "discord", "ghostty-wave", "spotify"]),
		).toEqual([1, 2, 3]);
	});
});

describe("ClaimSet.reset (claim_reset)", () => {
	test("clears claimed state so a window can be re-claimed", () => {
		const cs = new ClaimSet(profile);
		const windows = [win(1, "Arc")];
		expect(cs.claim(windows, "arc")).toBe(1);
		expect(cs.claim(windows, "arc")).toBeNull();
		cs.reset();
		expect(cs.claim(windows, "arc")).toBe(1);
	});
});
