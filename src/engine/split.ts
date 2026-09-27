import type {
	DisplayName,
	Profile,
	TrackKind,
	Weights,
} from "../config/types.ts";
import type { WorldSnapshot } from "./world.ts";

/** Layout override → display default → profile default → equal weights, keyed by kind + count. */
export function weightsFor(
	profile: Profile,
	kind: TrackKind,
	count: number,
	display: DisplayName | undefined,
	override?: Weights,
): number[] {
	if (override != null) {
		return [...override];
	}
	const displayWeights =
		display == null
			? undefined
			: profile.displays[display].weights?.[kind]?.[count];
	const profileWeights = profile.weights?.[kind]?.[count];
	const selected = displayWeights ?? profileWeights;
	return selected == null
		? Array.from({ length: count }, () => 1)
		: [...selected];
}

/** The profile display holding `space`, matched by width; unknown → undefined. */
export function displayOfSpace(
	profile: Profile,
	world: WorldSnapshot,
	space: string,
): DisplayName | undefined {
	const idx = world.spaces.find((s) => s.id === space)?.displayIdx;
	const width = world.displays.find((d) => d.idx === idx)?.frame.w;
	for (const [name, spec] of Object.entries(profile.displays)) {
		if (spec.width === width) {
			return name as DisplayName;
		}
	}
	return undefined;
}
