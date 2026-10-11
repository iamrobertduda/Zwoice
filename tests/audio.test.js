import { test } from "node:test";
import assert from "node:assert/strict";
import { distanceGain, opusSDP } from "../internal/web/assets/voice.js";

test("fade is continuous, bounded and monotone; exact endpoints", () => {
  assert.equal(distanceGain(10), 1);
  assert.equal(distanceGain(35), 0.5);
  assert.equal(distanceGain(60), 0);
  assert.equal(distanceGain(NaN), 0);
  let previous = 1;
  for (let d = 0; d < 100; d += 0.1) {
    const gain = distanceGain(d);
    assert.ok(gain <= previous && gain >= 0 && gain <= 1);
    previous = gain;
  }
});
test("Opus DTX/FEC negotiation preserves other codecs and is idempotent", () => {
  const source =
    "v=0\r\na=rtpmap:111 opus/48000/2\r\na=fmtp:111 minptime=10;useinbandfec=0\r\na=rtpmap:0 PCMU/8000\r\n";
  const result = opusSDP(source);
  assert.match(result, /usedtx=1/);
  assert.match(result, /maxaveragebitrate=32000/);
  assert.match(result, /minptime=10/);
  assert.match(result, /a=rtpmap:0 PCMU\/8000\r\n/);
  assert.equal(opusSDP(result), result);
});
