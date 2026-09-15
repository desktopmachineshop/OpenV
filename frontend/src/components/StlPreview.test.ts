import { modelExtent, parseStl } from './StlPreview';

// One 10 x 20 x 0 triangle, written both ways, is enough to check that the
// reader finds the corners, works out the bounding box, and recovers a facet
// normal without trusting the one in the file — which plenty of exporters
// write zeroed.
const CORNERS = [0, 0, 0, 10, 0, 0, 0, 20, 0];

const binaryStl = (triangles: number[][], zeroNormals = true): ArrayBuffer => {
  const buf = new ArrayBuffer(84 + triangles.length * 50);
  const view = new DataView(buf);
  view.setUint32(80, triangles.length, true);
  triangles.forEach((t, i) => {
    const at = 84 + i * 50;
    if (!zeroNormals) view.setFloat32(at, 1, true);
    for (let c = 0; c < 9; c++) view.setFloat32(at + 12 + c * 4, t[c], true);
  });
  return buf;
};

const asciiStl = (triangles: number[][]): ArrayBuffer => {
  const body = triangles
    .map(
      (t) => `facet normal 0 0 0
    outer loop
      vertex ${t[0]} ${t[1]} ${t[2]}
      vertex ${t[3]} ${t[4]} ${t[5]}
      vertex ${t[6]} ${t[7]} ${t[8]}
    endloop
  endfacet`
    )
    .join('\n');
  return ascii(`solid part\n${body}\nendsolid part\n`);
};

/** ASCII bytes, without assuming the test environment has a TextEncoder. */
const ascii = (text: string): ArrayBuffer => {
  const out = new Uint8Array(text.length);
  for (let i = 0; i < text.length; i++) out[i] = text.charCodeAt(i);
  return out.buffer;
};

describe('parseStl', () => {
  it('reads a binary STL', () => {
    const model = parseStl(binaryStl([CORNERS]));
    expect(model).not.toBeNull();
    expect(model!.facets).toHaveLength(1);
    expect(Array.from(model!.facets[0].v)).toEqual(CORNERS);
  });

  it('reads an ASCII STL', () => {
    const model = parseStl(asciiStl([CORNERS]));
    expect(model).not.toBeNull();
    expect(model!.facets).toHaveLength(1);
    expect(Array.from(model!.facets[0].v)).toEqual(CORNERS);
  });

  it('works the normal out from the corners rather than trusting the file', () => {
    // Both files declare a zero or wrong normal; a facet in the XY plane can
    // only face along Z, and a zero normal would render the part black.
    for (const data of [binaryStl([CORNERS]), asciiStl([CORNERS])]) {
      const n = parseStl(data)!.facets[0].n;
      expect(Math.hypot(n[0], n[1], n[2])).toBeCloseTo(1, 5);
      expect(Math.abs(n[2])).toBeCloseTo(1, 5);
    }
  });

  it('measures the model, which is what a reviewer checks first', () => {
    const model = parseStl(binaryStl([CORNERS, [0, 0, 0, 0, 0, -5, 3, 3, 3]]))!;
    expect(model.bounds.min).toEqual([0, 0, -5]);
    expect(model.bounds.max).toEqual([10, 20, 3]);
    expect(modelExtent(model.bounds)).toEqual([10, 20, 8]);
  });

  it('tells binary from ASCII by the length the header claims', () => {
    // An ASCII file starting with "solid" is the easy case to get wrong: the
    // first 80 bytes would be read as a binary header.
    const model = parseStl(asciiStl([CORNERS, CORNERS, CORNERS]));
    expect(model!.facets).toHaveLength(3);
  });

  it('refuses what is not an STL at all', () => {
    expect(parseStl(ascii('%PDF-1.7 not a mesh'))).toBeNull();
    expect(parseStl(new ArrayBuffer(0))).toBeNull();
  });

  it('reports a mesh it had to sample rather than pretending it is whole', () => {
    // Above the drawing budget the reader takes an even spread; the panel says
    // so, because a triangle count a reader might quote has to be honest.
    const many = Array.from({ length: 70000 }, () => CORNERS);
    const model = parseStl(binaryStl(many))!;
    expect(model.truncated).toBe(true);
    expect(model.facets.length).toBeLessThan(many.length);
    expect(model.facets.length).toBeGreaterThan(0);

    const few = parseStl(binaryStl([CORNERS]))!;
    expect(few.truncated).toBe(false);
  });
});
