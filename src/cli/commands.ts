import * as Context from "effect/Context";
import { Argument, Command as CliCommand, Flag } from "effect/cli";
import * as Effect from "effect/Effect";
import type * as Layer from "effect/Layer";
import type { Profile } from "../config/types.ts";
import type { WmDriver } from "../driver/types.ts";
import type { Command, RunOpts } from "../index.ts";
import { run } from "../index.ts";
import { DriverError, ProfileLoadError } from "./errors.ts";
import { commandFor, SUBCOMMANDS } from "./grammar.ts";

export interface RunDepsShape {
	readonly driver: WmDriver;
	readonly loadProfile: () => Promise<Profile>;
	readonly opts: RunOpts;
}

export class RunDeps extends Context.Service<RunDeps, RunDepsShape>()(
	"tess/RunDeps",
) {}

export const dispatch = (
	command: Command,
): Effect.Effect<void, ProfileLoadError | DriverError, RunDeps> =>
	Effect.gen(function* () {
		const deps = yield* RunDeps;
		const profile = yield* Effect.tryPromise({
			try: deps.loadProfile,
			catch: (cause) => new ProfileLoadError({ cause }),
		});
		const code = yield* Effect.tryPromise({
			try: () => run(profile, command, deps.driver, deps.opts),
			catch: (cause) => new DriverError({ cause }),
		});
		if (code !== 0) process.exitCode = code;
	});

const noArgument = (
	name:
		| "apply"
		| "laptop"
		| "display-event"
		| "flex-event"
		| "rules"
		| "display-setup"
		| "reset-splits"
		| "columns"
		| "toggle-float"
		| "balance",
) =>
	CliCommand.make(name, {}, () => dispatch(commandFor(name))).pipe(
		CliCommand.withDescription(SUBCOMMANDS[name].description),
	);

const literalArgument = (
	name:
		| "snap"
		| "stack-cycle"
		| "resize"
		| "move-display"
		| "cycle-display"
		| "focus"
		| "swap"
		| "warp"
		| "insert"
		| "space",
) =>
	CliCommand.make(
		name,
		{ value: Argument.Literals("value", SUBCOMMANDS[name].choices ?? []) },
		({ value }) => dispatch(commandFor(name, value)),
	).pipe(CliCommand.withDescription(SUBCOMMANDS[name].description));

const generated = [
	noArgument("apply"),
	noArgument("laptop"),
	noArgument("display-event"),
	noArgument("flex-event"),
	noArgument("rules"),
	noArgument("display-setup"),
	CliCommand.make("focus-slot", { n: Argument.Int("n") }, ({ n }) =>
		dispatch(commandFor("focus-slot", n)),
	).pipe(CliCommand.withDescription(SUBCOMMANDS["focus-slot"].description)),
	literalArgument("snap"),
	literalArgument("stack-cycle"),
	literalArgument("resize"),
	literalArgument("move-display"),
	literalArgument("cycle-display"),
	noArgument("reset-splits"),
	noArgument("columns"),
	literalArgument("focus"),
	literalArgument("swap"),
	literalArgument("warp"),
	literalArgument("insert"),
	noArgument("toggle-float"),
	noArgument("balance"),
	literalArgument("space"),
] as const satisfies ReadonlyArray<CliCommand.Command.SubcommandEntry>;

// Temporary until resident-daemon.md lands.
const selfFlag = Flag.String("self").pipe(
	Flag.filter(
		(value) => value !== "" && !value.startsWith("-"),
		() => "init --self needs a path",
	),
);
const initCommand = CliCommand.make("init", { self: selfFlag }, ({ self }) =>
	dispatch({ kind: "init", self }),
).pipe(CliCommand.withDescription("Register yabai signals and run startup."));

export const rootCommand: CliCommand.Command<
	"tess",
	Record<string, never>,
	Record<string, never>,
	ProfileLoadError | DriverError,
	RunDeps
> = CliCommand.make("tess").pipe(
	CliCommand.withDescription("Tessera window-manager driver"),
	CliCommand.withSubcommands([...generated, initCommand]),
);

export const withDeps = <E>(
	layer: Layer.Layer<RunDeps, E>,
): CliCommand.Command<
	"tess",
	Record<string, never>,
	Record<string, never>,
	ProfileLoadError | DriverError | E,
	never
> => rootCommand.pipe(CliCommand.provide(layer));
