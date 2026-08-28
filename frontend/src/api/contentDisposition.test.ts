import { filenameFromContentDisposition } from './contentDisposition';

describe('filenameFromContentDisposition', () => {
  it('returns null for missing headers', () => {
    expect(filenameFromContentDisposition(undefined)).toBeNull();
    expect(filenameFromContentDisposition(null)).toBeNull();
    expect(filenameFromContentDisposition('')).toBeNull();
    expect(filenameFromContentDisposition('attachment')).toBeNull();
  });

  it('parses a plain unquoted filename', () => {
    expect(filenameFromContentDisposition('attachment; filename=report.pdf')).toBe('report.pdf');
  });

  it('parses a quoted filename', () => {
    expect(filenameFromContentDisposition('attachment; filename="my export.json"')).toBe(
      'my export.json'
    );
  });

  it('keeps semicolons inside a quoted filename', () => {
    expect(filenameFromContentDisposition('attachment; filename="a;b.txt"; x=y')).toBe('a;b.txt');
  });

  it('stops an unquoted filename at the next parameter', () => {
    expect(
      filenameFromContentDisposition("attachment; filename=export.json; filename*=UTF-8''export.json")
    ).toBe('export.json');
  });

  it('prefers the RFC 5987 filename*= form and percent-decodes it', () => {
    expect(
      filenameFromContentDisposition(
        "attachment; filename=fallback.json; filename*=UTF-8''na%C3%AFve%20plan.json"
      )
    ).toBe('naïve plan.json');
  });

  it('parses filename*= when it is the only parameter', () => {
    expect(filenameFromContentDisposition("attachment; filename*=UTF-8''r%C3%A9sum%C3%A9.pdf")).toBe(
      'résumé.pdf'
    );
  });

  it('falls back to filename= when filename*= is malformed', () => {
    expect(
      filenameFromContentDisposition("attachment; filename=safe.json; filename*=UTF-8''bad%zz")
    ).toBe('safe.json');
  });

  it('is case-insensitive', () => {
    expect(filenameFromContentDisposition('attachment; FILENAME="Upper.txt"')).toBe('Upper.txt');
  });
});
