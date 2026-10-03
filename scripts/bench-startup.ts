import { chmodSync, mkdirSync, mkdtempSync, rmSync, statSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { spawnSync } from "node:child_process";

const [baseArg, newArg, countArg] = process.argv.slice(2);
if (!baseArg || !newArg || process.argv.length > 5) {
	console.error("usage: bun scripts/bench-startup.ts <base-binary> <new-binary> [n=40]");
	process.exit(2);
}
const count = countArg === undefined ? 40 : Number(countArg);
if (!Number.isSafeInteger(count) || count < 1) {
	console.error("n must be a positive integer");
	process.exit(2);
}
const resolveBinary = (path: string): string => {
	const absolute = resolve(path);
	return statSync(absolute).isDirectory() ? join(absolute, "bin", "tess") : absolute;
};
const base = resolveBinary(baseArg);
const current = resolveBinary(newArg);
const temp = mkdtempSync(join(tmpdir(), "tess-startup-"));
const home = join(temp, "home");
const shim = join(temp, "yabai");
const marker = join(temp, "shim-ran");
const size = (path: string): number => Bun.file(path).size;
const percentile = (values: ReadonlyArray<number>, p: number): number =>
	values[Math.ceil(values.length * p) - 1]!;
const ms = (value: number): string => value.toFixed(1);
const rows: { binary: string; command: string; p50: number; p90: number; min: number }[] = [];

try {
	mkdirSync(home, { recursive: true });
	writeFileSync(shim, '#!/bin/sh\nprintf x > "$TESS_BENCH_MARKER"\nexit 1\n');
	chmodSync(shim, 0o755);

	for (const [binary, name] of [[base, "base"], [current, "new"]] as const) {
		writeFileSync(marker, "");
		const probe = spawnSync(binary, ["snap", "3col"], {
			cwd: temp,
			env: { ...process.env, HOME: home, TESS_YABAI: shim, TESS_BENCH_MARKER: marker },
			encoding: "utf8",
		});
		if (probe.error) throw probe.error;
		if (probe.status !== 0) throw new Error(`${name} shim probe exited ${probe.status}: ${probe.stderr.trim()}`);
		if (!Bun.file(marker).size) {
			throw new Error(`${name} binary did not invoke TESS_YABAI; refusing an invalid comparison`);
		}
	}

	for (const [binary, name, commands] of [
		[base, "base", [["snap", "3col"], ["focus", "east"]]],
		[current, "new", [["snap", "3col"], ["focus", "east"], ["snap", "--help"]]],
	] as const) {
		for (const args of commands) {
			const times: number[] = [];
			for (let i = 0; i < count; i++) {
				writeFileSync(marker, "");
				const start = performance.now();
				const result = spawnSync(binary, [...args], {
					cwd: temp,
					env: { ...process.env, HOME: home, TESS_YABAI: shim, TESS_BENCH_MARKER: marker },
					encoding: "utf8",
				});
				const elapsed = performance.now() - start;
				if (result.error) throw result.error;
				if (result.status !== 0) {
					throw new Error(`${name} ${args.join(" ")} exited ${result.status}: ${result.stderr.trim()}`);
				}
				if (args[0] === "snap" && args[1] === "3col" && !Bun.file(marker).size) {
					throw new Error(`${name} binary stopped invoking TESS_YABAI; refusing an invalid measurement`);
				}
				times.push(elapsed);
			}
			times.sort((a, b) => a - b);
			rows.push({ binary: name, command: args.join(" "), p50: percentile(times, 0.5), p90: percentile(times, 0.9), min: times[0]! });
		}
	}

	const baseSize = size(base);
	const newSize = size(current);
	const snapBase = rows.find((row) => row.binary === "base" && row.command === "snap 3col")!;
	const focusBase = rows.find((row) => row.binary === "base" && row.command === "focus east")!;
	const snapNew = rows.find((row) => row.binary === "new" && row.command === "snap 3col")!;
	const focusNew = rows.find((row) => row.binary === "new" && row.command === "focus east")!;
	const helpNew = rows.find((row) => row.binary === "new" && row.command === "snap --help")!;
	const hotRatio = Math.max(snapNew.p50 / snapBase.p50, focusNew.p50 / focusBase.p50);
	const bars = [
		["hot-path p50 ratio ≤ 2.0×", hotRatio <= 2],
		["hot-path p90 ≤ 45 ms", snapNew.p90 <= 45 && focusNew.p90 <= 45],
		["help p50 ≤ 60 ms", helpNew.p50 <= 60],
		["binary size ≤ 70 MB", newSize <= 70_000_000],
	] as const;

	console.log("| binary | command | p50 (ms) | p90 (ms) | min (ms) | size (MB) |");
	console.log("| --- | --- | ---: | ---: | ---: | ---: |");
	for (const row of rows) {
		console.log(`| ${row.binary} | \`${row.command}\` | ${ms(row.p50)} | ${ms(row.p90)} | ${ms(row.min)} | ${((row.binary === "base" ? baseSize : newSize) / 1_000_000).toFixed(2)} |`);
	}
	console.log(`\nHot-path p50 ratio (worst row): ${hotRatio.toFixed(2)}×`);
	console.log(`\nBinary sizes: base ${baseSize} bytes; new ${newSize} bytes.`);
	for (const [label, passed] of bars) console.log(`${passed ? "PASS" : "FAIL"} ${label}`);
	if (bars.some(([, passed]) => !passed)) process.exitCode = 1;
} finally {
	rmSync(temp, { recursive: true, force: true });
}
