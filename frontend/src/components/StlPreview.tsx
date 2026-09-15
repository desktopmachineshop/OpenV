import React, { useEffect, useMemo, useRef, useState } from 'react';

// A look at the model, without a 3D engine.
//
// STL is the one CAD format that is both everywhere and trivially readable: a
// flat list of triangles, in one of two encodings, with no assemblies,
// materials, history or units to interpret. That makes a preview worth having
// here — a reviewer can see whether the bracket cited by a requirement is the
// bracket they think it is without opening a CAD seat — and cheap enough to
// write rather than to take a rendering library and its several hundred
// kilobytes into the bundle for one screen.
//
// What this is not: a CAD viewer. STEP, IGES and every native format need a
// geometry kernel to say anything at all about, so they are downloaded and
// opened in the tool that owns them. Showing an approximation of those would
// be worse than showing nothing, because a reviewer would trust it.

/** One triangle: its three corners and the facet normal, all in model space. */
interface Facet {
  /** Nine numbers: x, y, z for each of the three corners. */
  v: Float32Array;
  /** Unit normal, recomputed from the corners rather than trusted. */
  n: [number, number, number];
}

interface Bounds {
  min: [number, number, number];
  max: [number, number, number];
}

export interface StlModel {
  facets: Facet[];
  bounds: Bounds;
  /** Set when the file was readable but held more triangles than we drew. */
  truncated: boolean;
}

/**
 * The most triangles worth drawing.
 *
 * Every facet is sorted and painted on a 2D canvas each frame, so cost is
 * linear in facet count and a million-triangle mesh would drop the drag to a
 * slideshow. Past this the model is drawn from an evenly-spread sample, which
 * keeps the shape recognisable — which is all the preview claims to be — and
 * the panel says so rather than pretending it is complete.
 */
const MAX_FACETS = 60000;

const cross = (
  ax: number, ay: number, az: number,
  bx: number, by: number, bz: number
): [number, number, number] => [ay * bz - az * by, az * bx - ax * bz, ax * by - ay * bx];

const normalise = (v: [number, number, number]): [number, number, number] => {
  const len = Math.hypot(v[0], v[1], v[2]);
  return len > 0 ? [v[0] / len, v[1] / len, v[2] / len] : [0, 0, 1];
};

/** Whether the bytes are a binary STL, by checking the length the header claims. */
const looksBinary = (data: ArrayBuffer): boolean => {
  if (data.byteLength < 84) return false;
  const count = new DataView(data).getUint32(80, true);
  // A binary STL is exactly 84 bytes of preamble plus 50 per triangle. Some
  // writers pad the end, so the file may be longer but never shorter.
  return data.byteLength >= 84 + count * 50;
};

const parseBinary = (data: ArrayBuffer): { facets: Facet[]; truncated: boolean } => {
  const view = new DataView(data);
  const total = view.getUint32(80, true);
  const step = total > MAX_FACETS ? Math.ceil(total / MAX_FACETS) : 1;
  const facets: Facet[] = [];
  for (let i = 0; i < total; i += step) {
    const at = 84 + i * 50;
    if (at + 50 > data.byteLength) break;
    const v = new Float32Array(9);
    for (let c = 0; c < 9; c++) v[c] = view.getFloat32(at + 12 + c * 4, true);
    facets.push({ v, n: facetNormal(v) });
  }
  return { facets, truncated: step > 1 };
};

const parseAscii = (text: string): { facets: Facet[]; truncated: boolean } => {
  // "vertex x y z", three in a row, is the whole grammar that matters; the
  // facet normals in the file are ignored in favour of the corners, because
  // plenty of exporters write them zeroed or inconsistently wound.
  const numbers: number[] = [];
  const re = /vertex\s+(-?[\d.eE+-]+)\s+(-?[\d.eE+-]+)\s+(-?[\d.eE+-]+)/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(text)) !== null) {
    numbers.push(Number(m[1]), Number(m[2]), Number(m[3]));
  }
  const total = Math.floor(numbers.length / 9);
  const step = total > MAX_FACETS ? Math.ceil(total / MAX_FACETS) : 1;
  const facets: Facet[] = [];
  for (let i = 0; i < total; i += step) {
    const v = new Float32Array(numbers.slice(i * 9, i * 9 + 9));
    if (v.length < 9 || v.some((n) => !Number.isFinite(n))) continue;
    facets.push({ v, n: facetNormal(v) });
  }
  return { facets, truncated: step > 1 };
};

const facetNormal = (v: Float32Array): [number, number, number] =>
  normalise(
    cross(
      v[3] - v[0], v[4] - v[1], v[5] - v[2],
      v[6] - v[0], v[7] - v[1], v[8] - v[2]
    )
  );

/**
 * Read the bytes of an ASCII STL as text.
 *
 * The format is ASCII by definition, so this needs no decoder — which keeps
 * the reader free of an assumption about what the environment provides. The
 * chunking is not an optimisation: passing a multi-megabyte array to
 * String.fromCharCode in one call overflows the argument stack.
 */
const decodeAscii = (data: ArrayBuffer): string => {
  const bytes = new Uint8Array(data);
  const chunk = 32768;
  let out = '';
  for (let i = 0; i < bytes.length; i += chunk) {
    out += String.fromCharCode.apply(null, Array.from(bytes.subarray(i, i + chunk)));
  }
  return out;
};

/** Read an STL, whichever encoding it is in. Returns null for anything else. */
export const parseStl = (data: ArrayBuffer): StlModel | null => {
  let parsed: { facets: Facet[]; truncated: boolean };
  if (looksBinary(data)) {
    parsed = parseBinary(data);
  } else {
    const text = decodeAscii(data);
    if (!/^\s*solid/i.test(text) && !/vertex/i.test(text)) return null;
    parsed = parseAscii(text);
  }
  if (parsed.facets.length === 0) return null;

  const min: [number, number, number] = [Infinity, Infinity, Infinity];
  const max: [number, number, number] = [-Infinity, -Infinity, -Infinity];
  for (const f of parsed.facets) {
    for (let c = 0; c < 9; c += 3) {
      for (let axis = 0; axis < 3; axis++) {
        const val = f.v[c + axis];
        if (val < min[axis]) min[axis] = val;
        if (val > max[axis]) max[axis] = val;
      }
    }
  }
  return { facets: parsed.facets, bounds: { min, max }, truncated: parsed.truncated };
};

/** The model's overall size, which is what a reviewer usually wants to know. */
export const modelExtent = (b: Bounds): [number, number, number] => [
  b.max[0] - b.min[0],
  b.max[1] - b.min[1],
  b.max[2] - b.min[2],
];

interface StlPreviewProps {
  model: StlModel;
  /** Shown under the canvas: the file's own name, for the size caption. */
  unitsHint?: string;
}

/**
 * Draw the model, and let the reader turn it.
 *
 * Painter's algorithm on a 2D canvas: rotate every triangle into view space,
 * sort back to front, fill each with flat shading from a single light. No
 * depth buffer, which a convex-ish mechanical part does not miss, and no
 * WebGL context to lose.
 */
export const StlPreview: React.FC<StlPreviewProps> = ({ model, unitsHint }) => {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const [angle, setAngle] = useState({ yaw: -0.6, pitch: -1.0 });
  const drag = useRef<{ x: number; y: number } | null>(null);

  const centre = useMemo<[number, number, number]>(
    () => [
      (model.bounds.min[0] + model.bounds.max[0]) / 2,
      (model.bounds.min[1] + model.bounds.max[1]) / 2,
      (model.bounds.min[2] + model.bounds.max[2]) / 2,
    ],
    [model]
  );
  const radius = useMemo(() => {
    const [dx, dy, dz] = modelExtent(model.bounds);
    return Math.max(Math.hypot(dx, dy, dz) / 2, 1e-6);
  }, [model]);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ctx = canvas.getContext('2d');
    if (!ctx) return;

    // The canvas is sized in device pixels so the fill edges stay crisp on a
    // high-density screen, and scaled back for drawing in CSS pixels.
    const ratio = Math.min(window.devicePixelRatio || 1, 2);
    const w = canvas.clientWidth || 480;
    const h = canvas.clientHeight || 360;
    canvas.width = Math.round(w * ratio);
    canvas.height = Math.round(h * ratio);
    ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
    ctx.clearRect(0, 0, w, h);

    const cy = Math.cos(angle.yaw);
    const sy = Math.sin(angle.yaw);
    const cp = Math.cos(angle.pitch);
    const sp = Math.sin(angle.pitch);
    const scale = (Math.min(w, h) * 0.42) / radius;

    // Yaw about the vertical axis, then pitch, so dragging feels like turning
    // the part rather than the world.
    const project = (x: number, y: number, z: number): [number, number, number] => {
      const dx = x - centre[0];
      const dy = y - centre[1];
      const dz = z - centre[2];
      const rx = dx * cy - dy * sy;
      const ry = dx * sy + dy * cy;
      const rz = dz;
      const uy = ry * cp - rz * sp;
      const uz = ry * sp + rz * cp;
      return [w / 2 + rx * scale, h / 2 - uz * scale, uy];
    };

    type Drawn = { pts: [number, number][]; depth: number; shade: number };
    const drawn: Drawn[] = [];
    for (const f of model.facets) {
      const a = project(f.v[0], f.v[1], f.v[2]);
      const b = project(f.v[3], f.v[4], f.v[5]);
      const c = project(f.v[6], f.v[7], f.v[8]);
      // Rotate the normal the same way to light the facet as it now faces.
      const nx = f.n[0] * cy - f.n[1] * sy;
      const ny0 = f.n[0] * sy + f.n[1] * cy;
      const nz = f.n[2] * cp + ny0 * -sp;
      // A key light over the viewer's shoulder, and enough ambient that a
      // face turned away is still a face rather than a hole.
      const lit = Math.abs(nx * 0.35 + nz * 0.55 + 0.45);
      drawn.push({
        pts: [[a[0], a[1]], [b[0], b[1]], [c[0], c[1]]],
        depth: (a[2] + b[2] + c[2]) / 3,
        shade: Math.max(0.15, Math.min(1, lit)),
      });
    }
    drawn.sort((p, q) => p.depth - q.depth);

    for (const d of drawn) {
      const level = Math.round(70 + d.shade * 150);
      ctx.fillStyle = `rgb(${level}, ${Math.round(level * 1.02)}, ${Math.round(level * 1.08)})`;
      ctx.strokeStyle = ctx.fillStyle;
      ctx.lineWidth = 0.6;
      ctx.beginPath();
      ctx.moveTo(d.pts[0][0], d.pts[0][1]);
      ctx.lineTo(d.pts[1][0], d.pts[1][1]);
      ctx.lineTo(d.pts[2][0], d.pts[2][1]);
      ctx.closePath();
      ctx.fill();
      // Stroking with the fill colour closes the hairline seams that
      // antialiasing leaves between adjacent triangles.
      ctx.stroke();
    }
  }, [model, angle, centre, radius]);

  const onPointerDown = (e: React.PointerEvent<HTMLCanvasElement>) => {
    drag.current = { x: e.clientX, y: e.clientY };
    e.currentTarget.setPointerCapture(e.pointerId);
  };
  const onPointerMove = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (!drag.current) return;
    const dx = e.clientX - drag.current.x;
    const dy = e.clientY - drag.current.y;
    drag.current = { x: e.clientX, y: e.clientY };
    setAngle((a) => ({
      yaw: a.yaw + dx * 0.01,
      // Stop just short of the poles, where the model would flip over.
      pitch: Math.max(-Math.PI / 2 + 0.01, Math.min(Math.PI / 2 - 0.01, a.pitch + dy * 0.01)),
    }));
  };
  const endDrag = () => {
    drag.current = null;
  };

  const nudge = (e: React.KeyboardEvent<HTMLCanvasElement>) => {
    const step = 0.15;
    const moves: Record<string, () => void> = {
      ArrowLeft: () => setAngle((a) => ({ ...a, yaw: a.yaw - step })),
      ArrowRight: () => setAngle((a) => ({ ...a, yaw: a.yaw + step })),
      ArrowUp: () => setAngle((a) => ({ ...a, pitch: Math.max(-1.55, a.pitch - step) })),
      ArrowDown: () => setAngle((a) => ({ ...a, pitch: Math.min(1.55, a.pitch + step) })),
    };
    const move = moves[e.key];
    if (move) {
      e.preventDefault();
      move();
    }
  };

  const [dx, dy, dz] = modelExtent(model.bounds);
  const round = (n: number) => (n >= 100 ? n.toFixed(0) : n.toFixed(2));

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 8, alignItems: 'center' }}>
      <canvas
        ref={canvasRef}
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={endDrag}
        onPointerCancel={endDrag}
        onKeyDown={nudge}
        tabIndex={0}
        role="img"
        aria-label="Model preview. Drag, or use the arrow keys, to turn it."
        style={{
          width: 'min(560px, 80vw)',
          height: 'min(400px, 50vh)',
          maxWidth: '100%',
          background: 'var(--neutral-soft)',
          borderRadius: 6,
          border: '1px solid var(--border)',
          cursor: 'grab',
          touchAction: 'none',
        }}
      />
      <p style={{ margin: 0, fontSize: 12, color: 'var(--text-muted)', textAlign: 'center' }}>
        Drag to turn. {model.facets.length.toLocaleString()} triangles
        {model.truncated ? ' shown of a larger mesh' : ''} · bounding box{' '}
        {round(dx)} × {round(dy)} × {round(dz)}
        {unitsHint ? ` ${unitsHint}` : ''}
        {' '}· STL carries no units, so the numbers are whatever the exporter used.
      </p>
    </div>
  );
};

export default StlPreview;
