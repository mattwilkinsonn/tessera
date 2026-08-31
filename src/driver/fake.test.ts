// FakeDriver (T5): the named test for the D1 invariant — a space's stable
// SpaceId survives while its live index renumbers on destroy/create/move — plus
// the space-lifecycle + placement semantics the executor and converger rely on.

import { describe, expect, test } from "bun:test";
import { FakeDriver, type FakeSeed } from "./fake.ts";
import type { SpaceId } from "./types.ts";

/** A two-display seed: display 1 with 3 spaces, display 2 with 1 space. */
function seed(): FakeSeed {
	return {
		displays: [
			{ idx: 1, frame: { x: 0, y: 0, w: 3440, h: 1440 } },
			{ idx: 2, frame: { x: 0, y: 0, w: 1728, h: 1117 } },
		],
		spaces: [
			{ displayIdx: 1, label: "main" },
			{ displayIdx: 1, label: "" },
			{ displayIdx: 1, label: "lap-arc" },
			{ displayIdx: 2, label: "plan" },
		],
		windows: [
			{ id: 10, app: "Arc", spaceIndex: 1 },
			{ id: 11, app: "Ghostty", spaceIndex: 2 },
			{ id: 12, app: "Obsidian", spaceIndex: 3 },
			{ id: 20, app: "Linear", spaceIndex: 4 },
		],
	};
}

async function indexOf(d: FakeDriver, id: SpaceId): Promise<number> {
	const spaces = await d.querySpaces();
	const all = await d.queryDisplays();
	// Global index = position in the flat ordered space list + 1.
	const ordered = all.flatMap((disp) => disp.spaceIds);
	void spaces;
	return ordered.indexOf(id) + 1;
}

describe("FakeDriver D1 invariant (stable SpaceId survives index renumber)", () => {
	test("destroying a lower-index space renumbers later spaces but not their ids", async () => {
		const d = new FakeDriver(seed());
		const before = await d.querySpaces();
		const main = before[0]?.id as SpaceId;
		const stray = before[1]?.id as SpaceId;
		const lapArc = before[2]?.id as SpaceId;

		expect(await indexOf(d, lapArc)).toBe(3);

		// Destroy the middle (index-2) space on display 1.
		expect(await d.destroySpace(stray)).toBe(true);

		// lap-arc's stable id is unchanged, but its live index shifted 3 → 2.
		const after = await d.querySpaces();
		expect(after.some((s) => s.id === lapArc)).toBe(true);
		expect(await indexOf(d, lapArc)).toBe(2);
		// main is untouched at index 1.
		expect(await indexOf(d, main)).toBe(1);
	});

	test("createSpace mints a fresh id and appends within the display group", async () => {
		const d = new FakeDriver(seed());
		const created = await d.createSpace(2);
		expect(created).not.toBeNull();
		const spaces = await d.querySpaces();
		// The new space belongs to display 2, distinct id, empty label.
		const fresh = spaces.find((s) => s.id === created);
		expect(fresh?.displayIdx).toBe(2);
		expect(fresh?.label).toBe("");
		// Display 2 now has two spaces, the new one last in its group.
		const disp2 = (await d.queryDisplays()).find((x) => x.idx === 2);
		expect(disp2?.spaceIds.at(-1)).toBe(created as SpaceId);
	});

	test("moveSpaceToIndex reorders live index, id stable", async () => {
		const d = new FakeDriver(seed());
		const before = await d.querySpaces();
		const lapArc = before[2]?.id as SpaceId;
		await d.moveSpaceToIndex(lapArc, 1);
		expect(await indexOf(d, lapArc)).toBe(1);
		expect((await d.querySpaces()).some((s) => s.id === lapArc)).toBe(true);
	});
});

describe("FakeDriver space lifecycle", () => {
	test("destroySpace relocates residual windows to an adjacent same-display space", async () => {
		const d = new FakeDriver(seed());
		const before = await d.querySpaces();
		const stray = before[1]?.id as SpaceId; // holds window 11
		await d.destroySpace(stray);
		const wins = await d.queryWindows();
		const w11 = wins.find((w) => w.id === 11);
		// Window 11 survived, now on another display-1 space (not destroyed).
		expect(w11).toBeDefined();
		expect(w11?.displayIdx).toBe(1);
		expect(await d.querySpaces()).not.toContainEqual(
			expect.objectContaining({ id: stray }),
		);
	});

	test("refuses to destroy a display's sole space", async () => {
		const d = new FakeDriver(seed());
		const plan = (await d.querySpaces()).find((s) => s.label === "plan");
		expect(await d.destroySpace(plan?.id as SpaceId)).toBe(false);
		expect((await d.querySpaces()).some((s) => s.id === plan?.id)).toBe(true);
	});

	test("destroySpace on an unknown id is a false no-op", async () => {
		const d = new FakeDriver(seed());
		expect(await d.destroySpace("999" as SpaceId)).toBe(false);
	});

	test("labelSpace then restart clears labels (labels do not survive a restart)", async () => {
		const d = new FakeDriver(seed());
		const before = await d.querySpaces();
		const stray = before[1]?.id as SpaceId;
		await d.labelSpace(stray, "lap-ghostty");
		expect((await d.querySpaces()).find((s) => s.id === stray)?.label).toBe(
			"lap-ghostty",
		);
		d.restart();
		for (const s of await d.querySpaces()) {
			expect(s.label).toBe("");
		}
	});
});

describe("FakeDriver window placement + queries", () => {
	test("moveWindowToSpace updates the window's space and display", async () => {
		const d = new FakeDriver(seed());
		const plan = (await d.querySpaces()).find((s) => s.label === "plan");
		await d.moveWindowToSpace(10, plan?.id as SpaceId);
		const w10 = (await d.queryWindows()).find((w) => w.id === 10);
		expect(w10?.spaceId).toBe(plan?.id as SpaceId);
		expect(w10?.displayIdx).toBe(2);
	});

	test("queryWindowsOnSpace returns all windows on a space including after a move", async () => {
		const d = new FakeDriver(seed());
		const main = (await d.querySpaces())[0]?.id as SpaceId;
		await d.moveWindowToSpace(11, main);
		const on = await d.queryWindowsOnSpace(main);
		expect(on.map((w) => w.id).sort((a, b) => a - b)).toEqual([10, 11]);
	});

	test("realizeSpaceLayout moves column windows onto the space and unfloats them", async () => {
		const d = new FakeDriver(seed());
		const plan = (await d.querySpaces()).find((s) => s.label === "plan");
		await d.toggleFloat(10); // float Arc first
		expect((await d.queryWindows()).find((w) => w.id === 10)?.floating).toBe(
			true,
		);
		await d.realizeSpaceLayout(plan?.id as SpaceId, {
			kind: "2col",
			columns: [[10], [11]],
		});
		const wins = await d.queryWindows();
		for (const id of [10, 11]) {
			const w = wins.find((x) => x.id === id);
			expect(w?.spaceId).toBe(plan?.id as SpaceId);
			expect(w?.floating).toBe(false);
		}
	});

	test("toggleFloat flips floating; focusWindow sets the focused window", async () => {
		const d = new FakeDriver(seed());
		expect(await d.queryFocusedWindow()).toBeNull();
		expect(await d.focusWindow(20)).toBe(true);
		expect((await d.queryFocusedWindow())?.id).toBe(20);
		expect(await d.focusWindow(999)).toBe(false);
	});

	test("moveWindowToDisplay relocates to the target display's home space", async () => {
		const d = new FakeDriver(seed());
		expect(await d.moveWindowToDisplay(20, 1)).toBe(true);
		const w20 = (await d.queryWindows()).find((w) => w.id === 20);
		expect(w20?.displayIdx).toBe(1);
		// Landed on display 1's first (home) space.
		const disp1Home = (await d.queryDisplays()).find((x) => x.idx === 1)
			?.spaceIds[0];
		expect(w20?.spaceId).toBe(disp1Home as SpaceId);
	});

	test("settleMs defaults to 0 so tests never sleep", () => {
		expect(new FakeDriver().settleMs).toBe(0);
	});
});
