import { afterEach, describe, expect, test } from "bun:test";
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import * as Cause from "effect/Cause";
import {
	Command as CliCommand,
	CliConfig,
	CliError,
	GlobalFlag,
} from "effect/cli";
import * as Effect from "effect/Effect";
import * as Exit from "effect/Exit";
import * as FileSystem from "effect/FileSystem";
import * as Layer from "effect/Layer";
import * as Path from "effect/Path";
import * as ChildProcessSpawner from "effect/process/ChildProcessSpawner";
import * as Runtime from "effect/Runtime";
import * as Stdio from "effect/Stdio";
import * as Terminal from "effect/Terminal";
import { profile } from "../config/profile.fixture.ts";
import { FakeDriver } from "../driver/fake.ts";
import type { WmDriver } from "../driver/types.ts";
import type { Command, RunOpts } from "../index.ts";
import { run } from "../index.ts";
import { RunDeps, rootCommand, withDeps } from "./commands.ts";
import { DriverError, ProfileLoadError } from "./errors.ts";
import { SUBCOMMANDS } from "./grammar.ts";

const testRoots: Array<string> = [];
afterEach(() => {
	for (const root of testRoots) rmSync(root, { recursive: true, force: true });
	testRoots.length = 0;
	process.exitCode = 0;
});

function makeTestRoot(): string {
	const root = mkdtempSync(join(tmpdir(), "tess-cli-"));
	testRoots.push(root);
	return root;
}

const makeOpts = (root = makeTestRoot()) => ({
	applyLock: join(root, "apply.lock"),
	laptopLock: join(root, "laptop.lock"),
	guardPath: join(root, "guard"),
	flexPath: join(root, "flex"),
	displayStamp: join(root, "display-stamp"),
	flexStamp: join(root, "flex-stamp"),
	nudge: async () => {},
	displayWaiter: {
		waiterLock: join(root, "display-waiter-lock"),
		nudge: async () => {},
		deps: { now: () => 100, sleep: async () => {}, stamp: () => {} },
	},
	flexWaiter: {
		waiterLock: join(root, "flex-waiter-lock"),
		deps: { now: () => 100, sleep: async () => {}, stamp: () => {} },
	},
});

const stdoutChunks: Array<string> = [];
const cliServices = Layer.mergeAll(
	FileSystem.layerNoop({}),
	Path.layer,
	Stdio.layerTest({}),
	Layer.succeed(
		Terminal.Terminal,
		Terminal.make({
			columns: Effect.succeed(80),
			rows: Effect.succeed(24),
			readInput: Effect.die("unused"),
			readLine: Effect.die("unused"),
			display: (text) => Effect.sync(() => stdoutChunks.push(text)),
		}),
	),
	Layer.succeed(
		ChildProcessSpawner.ChildProcessSpawner,
		ChildProcessSpawner.make(() => Effect.die("unused")),
	),
	CliConfig.layer({ builtIns: [GlobalFlag.Help, GlobalFlag.Version] }),
);

const driverWith = (driver: WmDriver, opts: RunOpts = makeOpts()) =>
	Layer.succeed(RunDeps, { driver, loadProfile: async () => profile, opts });

const runWith = (
	argv: ReadonlyArray<string>,
	layer: Layer.Layer<RunDeps, never>,
) =>
	Effect.runPromiseExit(
		CliCommand.runWith(withDeps(layer), {
			version: "0.2.0",
			renderErrors: false,
		})(argv).pipe(Effect.provide(cliServices)),
	);

describe("Effect CLI commands", () => {
	type CliSubcommand = keyof typeof SUBCOMMANDS;
	const dispatchCases: ReadonlyArray<{
		name: CliSubcommand;
		args: string | undefined;
		command: Command;
	}> = [
		{ name: "apply", args: undefined, command: { kind: "apply" } },
		{ name: "laptop", args: undefined, command: { kind: "laptop" } },
		{
			name: "display-event",
			args: undefined,
			command: { kind: "display-event" },
		},
		{ name: "flex-event", args: undefined, command: { kind: "flex-event" } },
		{ name: "rules", args: undefined, command: { kind: "rules" } },
		{
			name: "display-setup",
			args: undefined,
			command: { kind: "display-setup" },
		},
		{ name: "focus-slot", args: "1", command: { kind: "focus-slot", n: 1 } },
		{ name: "snap", args: "3col", command: { kind: "snap", mode: "3col" } },
		{
			name: "stack-cycle",
			args: "next",
			command: { kind: "stack-cycle", dir: "next" },
		},
		{ name: "resize", args: "grow", command: { kind: "resize", dir: "grow" } },
		{
			name: "move-display",
			args: "g9",
			command: { kind: "move-display", name: "g9" },
		},
		{
			name: "cycle-display",
			args: "next",
			command: { kind: "cycle-display", dir: "next" },
		},
		{
			name: "reset-splits",
			args: undefined,
			command: { kind: "reset-splits" },
		},
		{ name: "columns", args: undefined, command: { kind: "columns" } },
		{ name: "focus", args: "west", command: { kind: "focus", dir: "west" } },
		{ name: "swap", args: "west", command: { kind: "swap", dir: "west" } },
		{ name: "warp", args: "west", command: { kind: "warp", dir: "west" } },
		{ name: "insert", args: "east", command: { kind: "insert", dir: "east" } },
		{
			name: "toggle-float",
			args: undefined,
			command: { kind: "toggle-float" },
		},
		{ name: "balance", args: undefined, command: { kind: "balance" } },
		{ name: "space", args: "bsp", command: { kind: "space", layout: "bsp" } },
	];
	test("dispatch cases cover every grammar subcommand", () => {
		expect(
			dispatchCases
				.map(({ name }) => name)
				.sort()
				.join("\n"),
		).toBe(Object.keys(SUBCOMMANDS).sort().join("\n"));
	});

	test("root command registers every grammar subcommand and init", () => {
		const names = rootCommand.subcommands.flatMap(({ commands }) =>
			commands.map(({ name }) => name),
		);
		expect(names.sort().join("\n")).toBe(
			[...Object.keys(SUBCOMMANDS), "init"].sort().join("\n"),
		);
	});

	const makeObservedDriver = () => {
		const calls: string[] = [];
		const driver = new Proxy(
			new FakeDriver({
				spaces: [{ displayIdx: 1 }],
				windows: [{ id: 7, app: "Arc", spaceIndex: 1 }],
			}),
			{
				get(target, property, receiver) {
					const member = Reflect.get(target, property, receiver) as unknown;
					if (typeof member !== "function") return member;
					return (...args: unknown[]) => {
						calls.push(`${String(property)}(${args.map(String).join(",")})`);
						return Reflect.apply(member, target, args);
					};
				},
			},
		);
		return { driver, calls };
	};

	for (const { name, args, command } of dispatchCases) {
		test(`dispatches ${name} through the shared router`, async () => {
			const { driver: driverA, calls: callsA } = makeObservedDriver();
			const { driver: driverB, calls: callsB } = makeObservedDriver();
			const optsA = makeOpts();
			const optsB = makeOpts();
			for (const opts of [optsA, optsB]) {
				if (name === "display-event" || name === "flex-event") {
					mkdirSync(opts.displayWaiter.waiterLock);
					writeFileSync(
						join(opts.displayWaiter.waiterLock, "pid"),
						`${process.pid}\n`,
					);
					mkdirSync(opts.flexWaiter.waiterLock);
					writeFileSync(
						join(opts.flexWaiter.waiterLock, "pid"),
						`${process.pid}\n`,
					);
				}
			}
			await Promise.all([driverA.focusWindow(7), driverB.focusWindow(7)]);
			callsA.length = 0;
			callsB.length = 0;
			const argv = args === undefined ? [name] : [name, args];
			const exit = await runWith(argv, driverWith(driverA, optsA));
			expect(Exit.isSuccess(exit)).toBe(true);
			const referenceExit = await run(profile, command, driverB, optsB);
			expect(referenceExit).toBe(0);
			expect(callsA).toEqual(callsB);
			if (name === "display-event") {
				expect(Bun.file(optsA.displayStamp).size > 0).toBe(true);
				expect(Bun.file(optsB.displayStamp).size > 0).toBe(true);
			}
			if (name === "flex-event") {
				expect(Bun.file(optsA.flexStamp).size > 0).toBe(true);
				expect(Bun.file(optsB.flexStamp).size > 0).toBe(true);
			}
		});
	}

	test("valid simple command changes the FakeDriver world", async () => {
		const driver = new FakeDriver({
			spaces: [{ displayIdx: 1 }],
			windows: [{ id: 7, app: "Arc", spaceIndex: 1 }],
		});
		await driver.focusWindow(7);
		const exit = await runWith(["toggle-float"], driverWith(driver));
		expect(Exit.isSuccess(exit)).toBe(true);
		expect((await driver.queryFocusedWindow())?.floating).toBe(true);
	});

	test("subcommand and root help do not build RunDeps; a valid command builds it once", async () => {
		let builds = 0;
		const counted = Layer.sync(RunDeps, () => {
			builds += 1;
			return {
				driver: new FakeDriver(),
				loadProfile: async () => profile,
				opts: makeOpts(),
			};
		});
		stdoutChunks.length = 0;
		for (const argv of [["snap", "--help"], ["--help"]]) {
			const exit = await Effect.runPromiseExit(
				CliCommand.runWith(withDeps(counted), {
					version: "0.2.0",
					renderErrors: false,
				})(argv).pipe(Effect.provide(cliServices)),
			);
			expect(Exit.isSuccess(exit)).toBe(true);
		}
		expect(stdoutChunks.join("")).not.toContain("--wizard");
		expect(builds).toBe(0);
		const valid = await Effect.runPromiseExit(
			CliCommand.runWith(withDeps(counted), {
				version: "0.2.0",
				renderErrors: false,
			})(["snap", "3col"]).pipe(Effect.provide(cliServices)),
		);
		expect(Exit.isSuccess(valid)).toBe(true);
		expect(builds).toBe(1);
	});

	test("invalid literals and invalid init flags carry parse errors with exit 1", async () => {
		for (const argv of [
			["snap", "bogus"],
			["init"],
			["init", "--self", ""],
			["init", "--self", "--apply"],
		]) {
			const exit = await runWith(argv, driverWith(new FakeDriver()));
			expect(Exit.isFailure(exit)).toBe(true);
			if (Exit.isFailure(exit)) {
				const error = Cause.squash(exit.cause);
				expect(error).toBeInstanceOf(CliError.ShowHelp);
				expect(Runtime.getErrorExitCode(error)).toBe(1);
			}
		}
	});

	test("malformed argv fails before calling the driver", async () => {
		for (const argv of [
			["bogus"],
			["snap"],
			["focus-slot"],
			["snap", "3col", "extra"],
			["focus", "west", "east"],
			["focus-slot", "x"],
		]) {
			const { driver, calls } = makeObservedDriver();
			const exit = await runWith(argv, driverWith(driver));
			expect(Exit.isFailure(exit), argv.join(" ")).toBe(true);
			if (Exit.isFailure(exit)) {
				const error = Cause.squash(exit.cause);
				expect(error).toBeInstanceOf(CliError.ShowHelp);
				expect(Runtime.getErrorExitCode(error)).toBe(1);
			}
			expect(calls, argv.join(" ")).toEqual([]);
		}
	});

	test("missing and throwing handlers use typed one-line error wrappers", async () => {
		const missing = Layer.succeed(RunDeps, {
			driver: new FakeDriver(),
			loadProfile: async () => {
				throw new Error("profile missing");
			},
			opts: makeOpts(),
		});
		const loadExit = await runWith(["focus", "east"], missing);
		expect(Exit.isFailure(loadExit)).toBe(true);
		if (Exit.isFailure(loadExit))
			expect(Cause.squash(loadExit.cause)).toBeInstanceOf(ProfileLoadError);

		const driver = new FakeDriver();
		driver.focusWindowDir = async () => {
			throw new Error("driver gone");
		};
		const driverExit = await runWith(
			["focus", "east"],
			Layer.succeed(RunDeps, {
				driver,
				loadProfile: async () => profile,
				opts: makeOpts(),
			}),
		);
		expect(Exit.isFailure(driverExit)).toBe(true);
		if (Exit.isFailure(driverExit))
			expect(Cause.squash(driverExit.cause)).toBeInstanceOf(DriverError);
	});

	test("laptop contention returns exit code 1 silently", async () => {
		const root = makeTestRoot();
		const laptopLock = join(root, "laptop.lock");
		mkdirSync(laptopLock);
		writeFileSync(join(laptopLock, "pid"), `${process.pid}\n`);
		process.exitCode = 0;
		const exit = await runWith(
			["laptop"],
			Layer.succeed(RunDeps, {
				driver: new FakeDriver({
					displays: [{ idx: 1, frame: { x: 0, y: 0, w: 1728, h: 1117 } }],
					spaces: [{ displayIdx: 1 }],
				}),
				loadProfile: async () => profile,
				opts: { ...makeOpts(root), laptopLock, nudge: async () => {} },
			}),
		);
		expect(Exit.isSuccess(exit)).toBe(true);
		expect(process.exitCode).toBe(1);
	});

	test("init passes --self to registered signal actions through the CLI", async () => {
		const registered: Array<{ event: string; command: string[] }> = [];
		const driver = new FakeDriver({
			displays: [{ idx: 1, frame: { x: 0, y: 0, w: 5120, h: 1440 } }],
			spaces: [{ displayIdx: 1 }],
		});
		Object.defineProperty(driver, "events", {
			value: {
				register: async (event: string, command: string[]) => {
					registered.push({ event, command });
				},
			},
		});
		const exit = await runWith(
			["init", "--self", "/tmp/x/tess"],
			driverWith(driver),
		);
		expect(Exit.isSuccess(exit)).toBe(true);
		expect(registered.length).toBeGreaterThan(0);
		const signalCommands = registered
			.filter(
				({ command }) =>
					command[1] === "display-event" || command[1] === "flex-event",
			)
			.map(({ command }) => command);
		expect(signalCommands.length).toBeGreaterThan(0);
		for (const command of signalCommands)
			expect(command[0]).toBe("/tmp/x/tess");
	});
});
