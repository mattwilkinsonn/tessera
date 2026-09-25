import type { Profile } from "./types.ts";

export function validateProfile(profile: Profile): void {
	const violations: string[] = [];
	for (const topology of profile.topologies ?? []) {
		const declared = new Set(topology.displays);
		const laidOut = new Set(topology.desk.map((layout) => layout.display));
		for (const display of declared) {
			if (!laidOut.has(display)) {
				violations.push(
					`topology "${topology.name}": no layout for ${display}`,
				);
			}
		}
		for (const display of laidOut) {
			if (!declared.has(display)) {
				violations.push(
					`topology "${topology.name}": layout for undeclared display ${display}`,
				);
			}
		}
	}
	if (violations.length > 0) {
		throw new Error(violations.join("; "));
	}
}
