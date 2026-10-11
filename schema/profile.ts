import { z } from "zod";

export const DisplayName = z.enum(["g9", "aw", "laptop"]);
export type DisplayName = z.infer<typeof DisplayName>;

export const TrackKind = z.enum(["columns", "rows"]);
export type TrackKind = z.infer<typeof TrackKind>;

export const Weights = z.array(z.number()).readonly();
export type Weights = z.infer<typeof Weights>;

export const WeightDefaults = z.strictObject({
  columns: z.record(z.string(), Weights).optional(),
  rows: z.record(z.string(), Weights).optional(),
});
export type WeightDefaults = z.infer<typeof WeightDefaults>;

export const WindowSpec = z.strictObject({
  app: z.string(),
  title: z.string().optional(),
  titleInvert: z.boolean().optional(),
  spawn: z.array(z.string()).readonly().optional(),
});
export type WindowSpec = z.infer<typeof WindowSpec>;

const Track = z.array(z.string()).readonly();
const Tracks = z.array(Track).readonly();

const StackLayout = z.strictObject({
  display: DisplayName,
  label: z.string(),
  kind: z.literal("stack"),
  tracks: z.tuple([Track]),
});

const TrackLayout = z.strictObject({
  display: DisplayName,
  label: z.string(),
  kind: TrackKind,
  tracks: Tracks,
  weights: Weights.optional(),
});

export const DeskLayout = z.discriminatedUnion("kind", [StackLayout, TrackLayout]);
export type DeskLayout = z.infer<typeof DeskLayout>;

export const DeskSlot = z.strictObject({
  name: z.string(),
  onDisplay: DisplayName.optional(),
});
export type DeskSlot = z.infer<typeof DeskSlot>;

export const Topology = z.strictObject({
  name: z.string(),
  displays: z.array(DisplayName).readonly(),
  desk: z.array(DeskLayout).readonly(),
});
export type Topology = z.infer<typeof Topology>;

export const Profile = z.strictObject({
  displays: z.strictObject({
    g9: z.strictObject({ width: z.number().int().positive(), weights: WeightDefaults.optional() }),
    aw: z.strictObject({ width: z.number().int().positive(), weights: WeightDefaults.optional() }),
    laptop: z.strictObject({ width: z.number().int().positive(), weights: WeightDefaults.optional() }),
  }),
  windows: z.record(z.string(), WindowSpec),
  desk: z.array(DeskLayout).readonly(),
  topologies: z.array(Topology).readonly().optional(),
  weights: WeightDefaults.optional(),
  deskSlots: z.array(DeskSlot).readonly(),
  laptopPinned: z.array(z.string()).readonly(),
  laptopStackApps: z.array(z.string()).readonly(),
});
export type Profile = z.infer<typeof Profile>;
