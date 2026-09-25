import type { Profile, WindowName } from "../config/types.ts";
import type { WmWindow } from "../driver/types.ts";
import { ClaimSet } from "./claim.ts";

/** Names whose ordered claims still need a newly opened app window. */
export function spawnsNeeded(
	profile: Profile,
	names: readonly WindowName[],
	windows: readonly WmWindow[],
): WindowName[] {
	const claims = new ClaimSet(profile);
	const needed: WindowName[] = [];
	for (const name of names) {
		if (
			claims.claim(windows, name) == null &&
			profile.windows[name]?.spawn != null
		) {
			needed.push(name);
		}
	}
	return needed;
}
