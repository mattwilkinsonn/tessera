/** The bsp chain ratios for a weight vector: r[i] = w[i] / sum(w[i..]). */
export function chainRatios(weights: ReadonlyArray<number>): number[] {
	// Huge finite weights can sum to Infinity; scale only then, since scaling
	// perturbs exact boundary ratios like 1/(1+9).
	const sum = weights.reduce((total, weight) => total + weight, 0);
	const max = Math.max(...weights);
	const scaled = Number.isFinite(sum)
		? weights
		: weights.map((weight) => weight / max);
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
