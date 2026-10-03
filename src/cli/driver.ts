import { YabaiDriver } from "../driver/yabai.ts";

export const liveDriver = (): YabaiDriver =>
	new YabaiDriver({ yabaiPath: process.env.TESS_YABAI });
