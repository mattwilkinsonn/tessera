import * as Data from "effect/Data";
import * as Runtime from "effect/Runtime";

export class ProfileLoadError extends Data.TaggedError("ProfileLoadError")<{
	readonly cause: unknown;
}> {
	readonly [Runtime.errorExitCode] = 1;
	readonly [Runtime.errorReported] = false;
}

export class DriverError extends Data.TaggedError("DriverError")<{
	readonly cause: unknown;
}> {
	readonly [Runtime.errorExitCode] = 1;
	readonly [Runtime.errorReported] = false;
}

export function formatDriverError(cause: unknown): string {
	return cause instanceof Error ? cause.message : String(cause);
}
