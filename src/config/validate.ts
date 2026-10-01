import { chainRatios } from "./chain.ts";
import type {
	DeskLayout,
	DisplayName,
	Profile,
	WeightDefaults,
	Weights,
} from "./types.ts";

const MIN_CHAIN_RATIO = 0.1;
const MAX_CHAIN_RATIO = 0.9;

function weightViolations(where: string, weights: Weights): string[] {
	const violations: string[] = [];
	if (weights.some((weight) => !Number.isFinite(weight) || weight <= 0)) {
		violations.push(`${where}: weights must be finite and greater than 0`);
	}
	const ratios = chainRatios(weights);
	for (let mask = 1; mask < 2 ** weights.length; mask++) {
		const subsequence = weights.filter(
			(_, index) => (mask & (1 << index)) !== 0,
		);
		if (subsequence.length < 2) {
			continue;
		}
		if (
			chainRatios(subsequence).some(
				(ratio) => ratio < MIN_CHAIN_RATIO || ratio > MAX_CHAIN_RATIO,
			)
		) {
			violations.push(
				`${where}: weight chain ratios must be between 0.1 and 0.9`,
			);
			break;
		}
	}
	if (ratios.length === 0 && weights.length > 1) {
		violations.push(
			`${where}: weight chain ratios must be between 0.1 and 0.9`,
		);
	}
	return violations;
}

function defaultsViolations(
	where: string,
	defaults: WeightDefaults | undefined,
): string[] {
	if (defaults == null) {
		return [];
	}
	const violations: string[] = [];
	for (const kind of ["columns", "rows"] as const) {
		for (const [key, weights] of Object.entries(defaults[kind] ?? {})) {
			const count = Number(key);
			const at = `${where} weights.${kind}.${key}`;
			if (!Number.isInteger(count) || count < 2 || count !== weights.length) {
				violations.push(
					`${at}: key must equal vector length and be at least 2`,
				);
			}
			violations.push(...weightViolations(at, weights));
		}
	}
	return violations;
}

function layoutViolations(where: string, layout: DeskLayout): string[] {
	const at = `${where} ${layout.label}`;
	const violations: string[] = [];
	if (layout.tracks.length === 0) {
		violations.push(`${at}: layout must have at least one track`);
	}
	for (const [index, track] of layout.tracks.entries()) {
		if (track.length === 0) {
			violations.push(
				`${at}: track ${index} must have at least one window name`,
			);
		}
	}
	if (layout.kind === "stack") {
		if (layout.tracks.length !== 1) {
			violations.push(`${at}: stack must have exactly one track`);
		}
		if (layout.weights != null) {
			violations.push(`${at}: stack cannot set weights`);
		}
	} else if (layout.tracks.length === 1 && layout.weights != null) {
		violations.push(`${at}: one-track layout cannot set weights`);
	}
	if (layout.weights != null) {
		if (layout.weights.length !== layout.tracks.length) {
			violations.push(`${at}: weights length must match tracks length`);
		}
		violations.push(...weightViolations(at, layout.weights));
	}
	return violations;
}

export function validateProfile(profile: Profile): void {
	const violations = defaultsViolations("profile", profile.weights);
	for (const [display, spec] of Object.entries(profile.displays)) {
		violations.push(...defaultsViolations(`display ${display}`, spec.weights));
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
