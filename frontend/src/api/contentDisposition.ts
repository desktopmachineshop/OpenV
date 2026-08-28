/**
 * Extracts the filename from a Content-Disposition header value.
 *
 * A naive `/filename=(.+)/` capture swallows everything to the end of the
 * header, including trailing parameters (e.g. `; filename*=UTF-8''…`). This
 * parser:
 *   1. prefers the RFC 5987 extended `filename*=charset'lang'percent-encoded`
 *      form (percent-decoded),
 *   2. then a quoted `filename="…"`,
 *   3. then an unquoted `filename=…`, stopping at the next `;`.
 *
 * Returns null when no filename can be extracted.
 */
export function filenameFromContentDisposition(header: string | null | undefined): string | null {
  if (!header) return null;

  // RFC 5987: filename*=UTF-8''some%20name.json (charset and language are
  // delimited by single quotes; the value is percent-encoded).
  const extended = header.match(/filename\*\s*=\s*([^';]*)'([^';]*)'([^;]+)/i);
  if (extended) {
    const value = extended[3].trim();
    try {
      const decoded = decodeURIComponent(value);
      if (decoded) return decoded;
    } catch {
      // Malformed percent-encoding — fall through to plain filename=.
    }
  }

  // Quoted form: filename="name; with special chars.json"
  const quoted = header.match(/filename\s*=\s*"([^"]*)"/i);
  if (quoted && quoted[1]) return quoted[1];

  // Unquoted form: filename=name.json; other=param
  const unquoted = header.match(/filename\s*=\s*([^;]+)/i);
  if (unquoted) {
    const value = unquoted[1].trim().replace(/^'(.*)'$/, '$1');
    if (value) return value;
  }

  return null;
}
