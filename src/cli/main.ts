import { BunRuntime, BunServices } from "@effect/platform-bun";
import { CliConfig, Command, GlobalFlag } from "effect/cli";
import * as Effect from "effect/Effect";
import * as Layer from "effect/Layer";
import pkg from "../../package.json" with { type: "json" };
import { loadProfile } from "../index.ts";
import { RunDeps, withDeps } from "./commands.ts";
import { formatDriverError, liveDriver } from "./driver.ts";
import { DriverError, ProfileLoadError } from "./errors.ts";

const printOneLine = (cause: unknown): Effect.Effect<void> =>
	Effect.sync(() => {
		if (cause instanceof ProfileLoadError || cause instanceof DriverError) {
			process.stderr.write(`${formatDriverError(cause.cause)}\n`);
		}
	});

export const main = (): void =>
	BunRuntime.runMain(
		withDeps(
			Layer.sync(RunDeps, () => ({
				driver: liveDriver(),
				loadProfile,
				opts: {},
			})),
		).pipe(
			Command.run({ version: pkg.version }),
			Effect.tapError(printOneLine),
			Effect.provide(
				Layer.mergeAll(
					BunServices.layer,
					CliConfig.layer({ builtIns: [GlobalFlag.Help, GlobalFlag.Version] }),
				),
			),
		),
	);
