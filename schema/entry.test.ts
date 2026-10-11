import { expect, test } from "bun:test";
import { Profile } from "tessera-wm";
import { profile } from "./default.ts";

// Imports through the package's own name, so a broken `exports` entry fails here.
test("the tessera-wm package entry exports a working Profile", () => {
  expect(Profile.parse(profile)).toEqual(profile);
});
