import type { DeskLayout, DisplayName, Profile } from "./types.ts";

// yabai's `--ratio abs:` accepts only values strictly between 0 and 1.
function badRatios(r: Profile["ratios"]): boolean {
	return [r.col3Root, r.col3Inner].some((v) => !(v > 0 && v < 1));
}

function layoutViolations(where: string, layout: DeskLayout): string[] {
	if (layout.ratios == null) {
		return [];
	}
	if (layout.kind !== "3col") {
		return [`${where} ${layout.label}: ratios only apply to a 3col layout`];
	}
	return badRatios(layout.ratios)
		? [`${where} ${layout.label}: ratios must be between 0 and 1`]
		: [];
}

export function validateProfile(profile: Profile): void {
	const violations: string[] = [];
	if (badRatios(profile.ratios)) {
		violations.push("profile ratios must be between 0 and 1");
	}
	for (const layout of profile.desk) {
		violations.push(...layoutViolations("desk", layout));
	}
	for (const topology of profile.topologies ?? []) {
		const declared = new Set(topology.displays);
		const laidOut = new Set<DisplayName>();
		for (const layout of topology.desk) {
			violations.push(
				...layoutViolations(`topology "${topology.name}"`, layout),
			);
			const { display } = layout;
			if (laidOut.has(display)) {
				violations.push(
					`topology "${topology.name}": duplicate layout for ${display}`,
				);
			}
			laidOut.add(display);
		}
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
