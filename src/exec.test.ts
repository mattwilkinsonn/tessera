import { describe, expect, test } from "bun:test";
import { FakeDriver } from "./driver/fake.ts";
import type { SpaceId } from "./driver/types.ts";
import type { ConvergeAction } from "./engine/laptop.ts";
import type { PlanOp } from "./engine/plan.ts";
import type { WorldSnapshot } from "./engine/world.ts";
import {
	type ConvergeStepFn,
	type ConvergeStepResult,
	runConverge,
	runPlan,
} from "./exec.ts";

const sid = (n: number): SpaceId => String(n) as SpaceId;

describe("runPlan", () => {
	test("relabelHome + realizeLayout builds the desk column on the labelled space", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [{ displayIdx: 1, label: "" }],
			windows: [
				{ id: 10, app: "Ghostty", spaceIndex: 1 },
				{ id: 11, app: "Zed", spaceIndex: 1, floating: true },
			],
		});
		const home = sid(1);
		const plan: PlanOp[] = [
			{ op: "relabelHome", homeSpace: home, label: "desk-code" },
			{
				op: "realizeLayout",
				space: home,
				target: { kind: "2col", columns: [[10], [11]] },
			},
		];
		await runPlan(driver, plan);

		const spaces = await driver.querySpaces();
		expect(spaces).toHaveLength(1);
		const space = spaces[0];
		if (space == null) throw new Error("no space");
		expect(space.label).toBe("desk-code");
		expect([...space.windowIds].sort((a, b) => a - b)).toEqual([10, 11]);
		const win11 = (await driver.queryWindows()).find((w) => w.id === 11);
		if (win11 == null) throw new Error("no win 11");
		expect(win11.floating).toBe(false);
	});

	test("createSpace mints a labelled space, moveWindow places a window on it", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [{ displayIdx: 1, label: "home" }],
			windows: [{ id: 20, app: "Safari", spaceIndex: 1 }],
		});
		// createSpace mints stable id 2 (nextSpaceId after seed space 1).
		const plan: PlanOp[] = [
			{ op: "createSpace", displayIdx: 1, label: "scratch" },
			{ op: "moveWindow", windowId: 20, toSpace: sid(2) },
		];
		await runPlan(driver, plan);

		const spaces = await driver.querySpaces();
		expect(spaces).toHaveLength(2);
		const scratch = spaces.find((s) => s.label === "scratch");
		if (scratch == null) throw new Error("no scratch space");
		expect(scratch.id).toBe(sid(2));
		expect(scratch.windowIds).toEqual([20]);
	});

	test("moveSpace reorders spaces by live index", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [
				{ displayIdx: 1, label: "a" },
				{ displayIdx: 1, label: "b" },
				{ displayIdx: 1, label: "c" },
			],
		});
		// Move space "c" (stable id 3) to index 1 (front).
		await runPlan(driver, [{ op: "moveSpace", space: sid(3), toIndex: 1 }]);

		const labels = (await driver.querySpaces()).map((s) => s.label);
		expect(labels).toEqual(["c", "a", "b"]);
	});

	test("destroySpace removes a non-last space", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [
				{ displayIdx: 1, label: "keep" },
				{ displayIdx: 1, label: "drop" },
			],
		});
		await runPlan(driver, [{ op: "destroySpace", space: sid(2) }]);

		const labels = (await driver.querySpaces()).map((s) => s.label);
		expect(labels).toEqual(["keep"]);
	});

	test("rehomeAndDestroy re-homes residual windows before destroying", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [
				{ displayIdx: 1, label: "home" },
				{ displayIdx: 1, label: "stale" },
			],
			windows: [
				{ id: 30, app: "Notes", spaceIndex: 2 },
				{ id: 31, app: "Mail", spaceIndex: 2 },
			],
		});
		await runPlan(driver, [
			{ op: "rehomeAndDestroy", staleSpace: sid(2), homeSpace: sid(1) },
		]);

		const spaces = await driver.querySpaces();
		expect(spaces).toHaveLength(1);
		const home = spaces[0];
		if (home == null) throw new Error("no home");
		expect(home.label).toBe("home");
		// Windows preserved on the home space — nothing closed.
		expect([...home.windowIds].sort((a, b) => a - b)).toEqual([30, 31]);
	});

	test("setLayout and balanceSpace drive their driver methods", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [{ displayIdx: 1, label: "s", layout: "bsp" }],
		});
		await runPlan(driver, [
			{ op: "setLayout", space: sid(1), layout: "stack" },
			{ op: "balanceSpace", space: sid(1) },
		]);

		const space = (await driver.querySpaces())[0];
		if (space == null) throw new Error("no space");
		expect(space.layout).toBe("stack");
	});

	test("exhaustiveness — every PlanOp kind is a valid runPlan input", async () => {
		// Keyed by every `PlanOp["op"]` tag: adding a union member without a
		// sample here is a compile error in THIS test (missing key), independent
		// of `applyOp`'s `never` default. So the vocabulary is guarded twice.
		const byOp: Record<PlanOp["op"], PlanOp> = {
			relabelHome: { op: "relabelHome", homeSpace: sid(1), label: "x" },
			createSpace: { op: "createSpace", displayIdx: 1, label: "x" },
			moveWindow: { op: "moveWindow", windowId: 1, toSpace: sid(1) },
			rehomeAndDestroy: {
				op: "rehomeAndDestroy",
				staleSpace: sid(1),
				homeSpace: sid(1),
			},
			moveSpace: { op: "moveSpace", space: sid(1), toIndex: 1 },
			setLayout: { op: "setLayout", space: sid(1), layout: "bsp" },
			destroySpace: { op: "destroySpace", space: sid(1) },
			realizeLayout: {
				op: "realizeLayout",
				space: sid(1),
				target: { kind: "stack", columns: [[1]] },
			},
			balanceSpace: { op: "balanceSpace", space: sid(1) },
		};
		const ops: readonly PlanOp[] = Object.values(byOp);
		const kinds = new Set(ops.map((o) => o.op));
		expect(kinds.size).toBe(ops.length);
	});

	test("onOp fires once before each op, in order", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [{ displayIdx: 1, label: "a" }],
		});
		const plan: PlanOp[] = [
			{ op: "relabelHome", homeSpace: sid(1), label: "b" },
			{ op: "setLayout", space: sid(1), layout: "stack" },
			{ op: "balanceSpace", space: sid(1) },
		];
		const seen: string[] = [];
		await runPlan(driver, plan, (op) => {
			seen.push(op.op);
		});
		// The hook fires once per op in plan order — a dropped hook leaves `seen`
		// empty.
		expect(seen).toEqual(["relabelHome", "setLayout", "balanceSpace"]);
		// The plan still ran: the layout landed.
		expect((await driver.querySpaces())[0]?.layout).toBe("stack");
	});

	test("onOp runs BEFORE each op's effect (a throw skips that op)", async () => {
		// The guard re-stamp must precede the op it protects, so onOp runs before
		// applyOp. Prove the ordering: throwing in the hook aborts the op's effect.
		// If onOp were moved to AFTER applyOp, the relabel would land before the
		// throw and this test would fail — the ordering the "fires in order" test
		// above cannot distinguish on its own.
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [{ displayIdx: 1, label: "a" }],
		});
		const plan: PlanOp[] = [
			{ op: "relabelHome", homeSpace: sid(1), label: "b" },
		];
		await expect(
			runPlan(driver, plan, () => {
				throw new Error("stop before effect");
			}),
		).rejects.toThrow("stop before effect");
		// The relabel never happened: the hook threw before applyOp ran.
		expect((await driver.querySpaces())[0]?.label).toBe("a");
	});
});

describe("runConverge", () => {
	// A hand-rolled converger: it names spaces by label and drives the FakeDriver
	// toward a fixed point — label the home space, create + fill a "code" space,
	// then declare done. It is world-driven (reads the fresh snapshot each turn)
	// exactly like the real laptop converger, so it exercises the LOOP contract
	// (query → step → execute → re-query, termination) without the converger's
	// internal phase logic (covered in laptop.test.ts).
	interface DemoState {
		readonly step: number;
	}

	const homeLabel = "conv-home";

	const demoStep: ConvergeStepFn<DemoState> = (
		world: WorldSnapshot,
		state: DemoState,
	): ConvergeStepResult<DemoState> => {
		const home = world.spaces[0];
		if (home == null) throw new Error("no home space");
		// 1: label the home space if not already.
		if (home.label !== homeLabel) {
			const action: ConvergeAction = {
				op: "relabelHome",
				homeSpace: home.id,
				label: homeLabel,
			};
			return { action, state: { step: state.step + 1 } };
		}
		// 2: ensure a "conv-code" space exists.
		const code = world.spaces.find((s) => s.label === "conv-code");
		if (code == null) {
			const action: ConvergeAction = {
				op: "createSpace",
				displayIdx: 1,
				label: "conv-code",
			};
			return { action, state: { step: state.step + 1 } };
		}
		// 3: ensure window 40 lives on the code space.
		const win = world.windows.find((w) => w.id === 40);
		if (win != null && win.spaceId !== code.id) {
			const action: ConvergeAction = {
				op: "moveWindow",
				windowId: 40,
				toSpace: code.id,
			};
			return { action, state: { step: state.step + 1 } };
		}
		return { done: true, state };
	};

	const seed = () =>
		new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [{ displayIdx: 1, label: "" }],
			windows: [{ id: 40, app: "Zed", spaceIndex: 1 }],
		});

	test("converges to the fixed point and is idempotent on a second run", async () => {
		const driver = seed();
		const final = await runConverge(driver, demoStep, { step: 0 });
		expect(final.step).toBe(3);

		const assertConverged = async () => {
			const spaces = await driver.querySpaces();
			const home = spaces.find((s) => s.label === homeLabel);
			const code = spaces.find((s) => s.label === "conv-code");
			if (home == null || code == null) throw new Error("not converged");
			expect(code.windowIds).toEqual([40]);
			return spaces.map((s) => ({ id: s.id, label: s.label }));
		};
		const after1 = await assertConverged();

		// Second run: already at the fixed point — the step fn returns done on the
		// FIRST turn, so nothing changes (idempotency).
		const final2 = await runConverge(driver, demoStep, { step: 0 });
		expect(final2.step).toBe(0);
		const after2 = await assertConverged();
		expect(after2).toEqual(after1);
	});

	test("throws the cap error on a non-terminating converger", async () => {
		const driver = seed();
		let n = 0;
		const forever: ConvergeStepFn<DemoState> = (
			world: WorldSnapshot,
		): ConvergeStepResult<DemoState> => {
			const home = world.spaces[0];
			if (home == null) throw new Error("no home");
			n += 1;
			// Always emits an action, never done — a converger bug.
			const action: ConvergeAction = {
				op: "relabelHome",
				homeSpace: home.id,
				label: `flip-${n % 2}`,
			};
			return { action, state: { step: n } };
		};
		expect(runConverge(driver, forever, { step: 0 })).rejects.toThrow(
			/exceeded 200 iterations/,
		);
	});
});
