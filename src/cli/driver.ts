import { YabaiDriver } from "../driver/yabai.ts";

export const liveDriver = (): YabaiDriver =>
	new YabaiDriver({ yabaiPath: process.env.TESS_YABAI });
export function formatDriverError(cause: unknown): string {
	return cause instanceof Error ? cause.message : String(cause);
}
