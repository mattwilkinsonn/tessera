/** The bsp chain ratios for a weight vector: r[i] = w[i] / sum(w[i..]). */
export function chainRatios(weights: ReadonlyArray<number>): number[] {
	const ratios: number[] = [];
	let remainder = weights.reduce((sum, weight) => sum + weight, 0);
	for (let i = 0; i < weights.length - 1; i++) {
		const weight = weights[i];
		if (weight === undefined) continue;
		ratios.push(weight / remainder);
		remainder -= weight;
	}
	return ratios;
}
