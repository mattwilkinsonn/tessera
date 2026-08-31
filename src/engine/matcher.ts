// Layer 2 — ENGINE: the single-engine window matcher.
//
// Pure: functions over values, no driver / filesystem / clock. Ports the
// window-match predicate (`win_id` matcher) and the slug
// resolver (`slug_for_window`) to ONE regex engine (JS
// `RegExp`), collapsing the Oniguruma-vs-POSIX-ERE divergence the bash corpus
// policed by convention. `RegExp.test` is unanchored (substring), matching the
// jq `test()` semantics the bash matcher used.

import type { Profile, WindowSpec } from "../config/types.ts";

/**
 * The window-match predicate (`win_id` matcher;
 * `slug_for_window` title logic).
 *
 * `spec.app` must match `app`; then the title rule:
 * - no `spec.title` → match any title (empty-title = match any);
 * - `spec.titleInvert` → match when `spec.title` does NOT match;
 * - otherwise → match when `spec.title` matches.
 *
 * Match invariant: profile specs MUST be plain (non-global) `RegExp`s — the
 * same literal-subset constraintalready documents. A `/g`
 * (or sticky `/y`) spec carries mutable `lastIndex` state on the RegExp object
 * across `.test` calls, which would make placement depend on call order and
 * silently mis-home windows. We defend it regardless: `lastIndex` is reset to
 * 0 before each `.test`, so a stray global flag cannot leak state between
 * calls.
 */
export function matchesSpec(
	spec: WindowSpec,
	app: string,
	title: string,
): boolean {
	spec.app.lastIndex = 0;
	if (!spec.app.test(app)) {
		return false;
	}
	if (spec.title == null) {
		return true;
	}
	spec.title.lastIndex = 0;
	const titleMatches = spec.title.test(title);
	return spec.titleInvert === true ? !titleMatches : titleMatches;
}

/**
 * Resolve a window's logical slug (`slug_for_window`).
 *
 * Iterate the WIN keys in SORTED order (`... | sort`) and return
 * the FIRST key whose spec matches `app`/`title` (same title logic as
 * `matchesSpec`). If none match, the fallback slug is `app` lowercased with each
 * maximal run of non-`[a-z0-9]` chars squeezed to a single `-` (`tr -cs
 * 'a-z0-9' '-'`), then a leading/trailing `-` stripped
 * (`${s#-}` / `${s%-}`).
 */
export function slugForWindow(
	profile: Profile,
	app: string,
	title: string,
): string {
	const keys = Object.keys(profile.windows).sort();
	for (const key of keys) {
		const spec = profile.windows[key];
		if (spec != null && matchesSpec(spec, app, title)) {
			return key;
		}
	}
	return app
		.toLowerCase()
		.replace(/[^a-z0-9]+/g, "-")
		.replace(/^-+/, "")
		.replace(/-+$/, "");
}
