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
import { RunDeps, withDeps } from "./commands.ts";
import { DriverError, ProfileLoadError } from "./errors.ts";
import { SUBCOMMANDS } from "./grammar.ts";

let testRoot = "";
afterEach(() => {
	if (testRoot !== "") rmSync(testRoot, { recursive: true, force: true });
	testRoot = "";
	process.exitCode = 0;
});

function makeTestRoot(): string {
	testRoot = mkdtempSync(join(tmpdir(), "tess-cli-"));
	return testRoot;
}

const makeOpts = (root = makeTestRoot()) => ({
	applyLock: join(root, "apply.lock"),
	laptopLock: join(root, "laptop.lock"),
	guardPath: join(root, "guard"),
	flexPath: join(root, "flex"),
	displayStamp: join(root, "display-stamp"),
	flexStamp: join(root, "flex-stamp"),
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

const driverWith = (driver: WmDriver, opts = makeOpts()) =>
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
	test.each(
		Object.entries(SUBCOMMANDS).map(
			([name, spec]) =>
				[name, spec.choices?.[0] ?? (spec.int ? "1" : undefined)] as const,
		),
	)("dispatches %s through the shared router", async (name, value) => {
		const driver = new FakeDriver({
			spaces: [{ displayIdx: 1 }],
			windows: [{ id: 7, app: "Arc", spaceIndex: 1 }],
		});
		const opts = makeOpts();
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
		const exit = await runWith(
			value === undefined ? [name] : [name, value],
			driverWith(driver, opts),
		);
		expect(Exit.isSuccess(exit)).toBe(true);
	});

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

	test("init accepts --self and creates a registered command", async () => {
		const driver = new FakeDriver();
		const exit = await runWith(
			["init", "--self", "/usr/local/bin/tess"],
			driverWith(driver),
		);
		expect(Exit.isSuccess(exit)).toBe(true);
	});
});
