/** The bsp chain ratios for a weight vector: r[i] = w[i] / sum(w[i..]). */
export function chainRatios(weights: ReadonlyArray<number>): number[] {
	// Huge finite weights can sum to Infinity; scale by a power of two then,
	// which is exact and so keeps boundary ratios like 1/(1+9) bit-identical.
	const sum = weights.reduce((total, weight) => total + weight, 0);
	const scale = Number.isFinite(sum)
		? 1
		: 2 ** -Math.ceil(Math.log2(Math.max(...weights)));
	const scaled =
		scale === 1 ? weights : weights.map((weight) => weight * scale);
	const ratios: number[] = [];
	let remainder = Number.isFinite(sum)
		? sum
		: scaled.reduce((total, weight) => total + weight, 0);
	for (let i = 0; i < weights.length - 1; i++) {
		const weight = scaled[i];
		if (weight === undefined) continue;
		ratios.push(weight / remainder);
		remainder -= weight;
	}
	return ratios;
}
