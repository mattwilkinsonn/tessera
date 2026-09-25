import type { DeskLayout, DisplayName, Profile, SplitRatios } from "./types.ts";

// yabai's `--ratio abs:` accepts only values strictly between 0 and 1.
function ratioViolations(
	where: string,
	ratios: SplitRatios | undefined,
): string[] {
	if (ratios == null) {
		return [];
	}
	const values = [ratios.col3?.col3Root, ratios.col3?.col3Inner, ratios.col2];
	return values.some((v) => v != null && !(v > 0 && v < 1))
		? [`${where}: ratios must be between 0 and 1`]
		: [];
}

const SPLIT_KEY = { "3col": "col3", "2col": "col2", stack: undefined } as const;

function layoutViolations(where: string, layout: DeskLayout): string[] {
	const at = `${where} ${layout.label}`;
	const violations = ratioViolations(at, layout.ratios);
	for (const key of ["col3", "col2"] as const) {
		if (layout.ratios?.[key] != null && key !== SPLIT_KEY[layout.kind]) {
			violations.push(
				`${at}: ratios.${key} does not apply to a ${layout.kind} layout`,
			);
		}
	}
	return violations;
}

export function validateProfile(profile: Profile): void {
	const violations = ratioViolations("profile", { col3: profile.ratios });
	for (const [display, spec] of Object.entries(profile.displays)) {
		violations.push(...ratioViolations(`display ${display}`, spec.ratios));
	}
	for (const layout of profile.desk) {
		violations.push(...layoutViolations("desk", layout));
	}
	for (const topology of profile.topologies ?? []) {
		const where = `topology "${topology.name}"`;
		const declared = new Set(topology.displays);
		const laidOut = new Set<DisplayName>();
		for (const layout of topology.desk) {
			violations.push(...layoutViolations(where, layout));
			if (laidOut.has(layout.display)) {
				violations.push(`${where}: duplicate layout for ${layout.display}`);
			}
			laidOut.add(layout.display);
		}
		for (const display of declared) {
			if (!laidOut.has(display)) {
				violations.push(`${where}: no layout for ${display}`);
			}
		}
		for (const display of laidOut) {
			if (!declared.has(display)) {
				violations.push(`${where}: layout for undeclared display ${display}`);
			}
		}
	}
	if (violations.length > 0) {
		throw new Error(violations.join("; "));
	}
}
