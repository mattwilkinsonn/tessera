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
	test("relabelHome + realizeLayout builds tracks on the labelled space", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [{ displayIdx: 1, label: "" }],
			windows: [
				{ id: 10, app: "Ghostty", spaceIndex: 1 },
				{ id: 11, app: "Zed", spaceIndex: 1, floating: true },
			],
		});
		const home = sid(1);
		await runPlan(driver, [
			{ op: "relabelHome", homeSpace: home, label: "desk-code" },
			{
				op: "realizeLayout",
				space: home,
				target: { kind: "columns", tracks: [[10], [11]], weights: [3, 2] },
			},
		]);
		const [desk] = await driver.querySpaces();
		expect(desk?.label).toBe("desk-code");
		expect([...(desk?.windowIds ?? [])].sort((a, b) => a - b)).toEqual([
			10, 11,
		]);
		expect(
			(await driver.queryWindows()).find((window) => window.id === 11)
				?.floating,
		).toBe(false);
	});

	test("createSpace mints a labelled space, moveWindow places a window on it", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [{ displayIdx: 1, label: "home" }],
			windows: [{ id: 20, app: "Safari", spaceIndex: 1 }],
		});
		await runPlan(driver, [
			{ op: "createSpace", displayIdx: 1, label: "scratch" },
			{ op: "moveWindow", windowId: 20, toSpace: sid(2) },
		]);
		const spaces = await driver.querySpaces();
		expect(spaces).toHaveLength(2);
		const scratch = spaces.find((space) => space.label === "scratch");
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
		await runPlan(driver, [{ op: "moveSpace", space: sid(3), toIndex: 1 }]);
		expect((await driver.querySpaces()).map((space) => space.label)).toEqual([
			"c",
			"a",
			"b",
		]);
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
		expect((await driver.querySpaces()).map((space) => space.label)).toEqual([
			"keep",
		]);
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
		expect((await driver.querySpaces())[0]?.layout).toBe("stack");
	});

	test("exhaustiveness — every PlanOp kind is a valid runPlan input", async () => {
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
				target: { kind: "stack", tracks: [[1]] },
			},
			balanceSpace: { op: "balanceSpace", space: sid(1) },
		};
		const ops: readonly PlanOp[] = Object.values(byOp);
		expect(new Set(ops.map((op) => op.op)).size).toBe(ops.length);
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
		expect(seen).toEqual(["relabelHome", "setLayout", "balanceSpace"]);
		expect((await driver.querySpaces())[0]?.layout).toBe("stack");
	});

	test("onOp runs before each op effect", async () => {
		const driver = new FakeDriver({
			displays: [{ idx: 1 }],
			spaces: [{ displayIdx: 1, label: "a" }],
		});
		await expect(
			runPlan(
				driver,
				[{ op: "relabelHome", homeSpace: sid(1), label: "b" }],
				() => {
					throw new Error("stop before effect");
				},
			),
		).rejects.toThrow("stop before effect");
		expect((await driver.querySpaces())[0]?.label).toBe("a");
	});
});

describe("runConverge", () => {
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
		if (home.label !== homeLabel) {
			const action: ConvergeAction = {
				op: "relabelHome",
				homeSpace: home.id,
				label: homeLabel,
			};
			return { action, state: { step: state.step + 1 } };
		}
		const code = world.spaces.find((space) => space.label === "conv-code");
		if (code == null) {
			const action: ConvergeAction = {
				op: "createSpace",
				displayIdx: 1,
				label: "conv-code",
			};
			return { action, state: { step: state.step + 1 } };
		}
		const window = world.windows.find((candidate) => candidate.id === 40);
		if (window != null && window.spaceId !== code.id) {
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
		expect((await runConverge(driver, demoStep, { step: 0 })).step).toBe(3);
		const assertConverged = async () => {
			const spaces = await driver.querySpaces();
			const home = spaces.find((space) => space.label === homeLabel);
			const code = spaces.find((space) => space.label === "conv-code");
			if (home == null || code == null) throw new Error("not converged");
			expect(code.windowIds).toEqual([40]);
			return spaces.map((space) => ({ id: space.id, label: space.label }));
		};
		const after1 = await assertConverged();
		expect((await runConverge(driver, demoStep, { step: 0 })).step).toBe(0);
		expect(await assertConverged()).toEqual(after1);
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
			const action: ConvergeAction = {
				op: "relabelHome",
				homeSpace: home.id,
				label: `flip-${n % 2}`,
			};
			return { action, state: { step: n } };
		};
		await expect(runConverge(driver, forever, { step: 0 })).rejects.toThrow(
			/exceeded 200 iterations/,
		);
	});
});
