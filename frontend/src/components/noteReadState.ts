import type React from 'react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { commentReadAPI, type ChatterEntry, type DomainEvent } from '../api/client';
import type { FeatureKey } from '../generated/contract';

// Comment read state (the comment-reads feature): when the member last read
// each artifact's comments. Needs you lists a comment only while it is
// unread, that is written after its artifact's read time; opening the
// artifact's comments, or Mark read, moves that time to now, and Mark unread
// moves it to just before a comment, so that comment and every later one on
// the artifact come back.

/** The gate on read state: the Mark read controls and the unread filter. */
export const COMMENT_READS_FEATURE: FeatureKey = 'comment-reads';

/**
 * Whether the member wrote the comment. Their own comments never need them,
 * whether or not read state is on: the event's actor is "user:<id>".
 */
export const isOwnComment = (c: Pick<DomainEvent, 'actor'>, userId: string | null | undefined): boolean =>
  Boolean(userId) && c.actor === `user:${userId}`;

/** When each of these artifacts' comments were last read: artifact id to milliseconds. */
export const loadCommentReads = async (artifactIds: string[]): Promise<Map<string, number>> => {
  const ids = [...new Set(artifactIds.filter(Boolean))].slice(0, 200);
  const out = new Map<string, number>();
  if (ids.length === 0) return out;
  const res = await commentReadAPI.list(ids);
  for (const r of Array.isArray(res.data?.comment_reads) ? res.data.comment_reads : []) {
    const at = Date.parse(r.read_at);
    if (!Number.isNaN(at)) out.set(r.artifact_id, at);
  }
  return out;
};

/** The id of the artifact a comment event is about. */
export const commentArtifact = (c: Pick<DomainEvent, 'payload'>): string => String(c.payload?.artifact_id || '');

/** Whether a comment was written after its artifact's comments were last read. */
export const isUnread = (c: Pick<DomainEvent, 'payload' | 'created_at'>, reads: Map<string, number>): boolean => {
  const read = reads.get(commentArtifact(c));
  return read === undefined || Date.parse(c.created_at) > read;
};

/**
 * The comments that need the member: someone else's, and with read state on,
 * unread ones only. Read state that could not be read leaves every comment
 * in, as before the feature.
 */
export const commentsNeedingMe = async (
  comments: DomainEvent[],
  opts: { userId?: string | null; readState?: boolean },
): Promise<DomainEvent[]> => {
  const others = comments.filter((c) => !isOwnComment(c, opts.userId));
  if (!opts.readState || others.length === 0) return others;
  const reads = await loadCommentReads(others.map(commentArtifact)).catch(() => new Map<string, number>());
  return others.filter((c) => isUnread(c, reads));
};

/** Mark these artifacts' comments read, now. */
export const markCommentsRead = (artifactIds: string[]) =>
  commentReadAPI.set([...new Set(artifactIds.filter(Boolean))].map((artifact_id) => ({ artifact_id })));

/**
 * Mark artifacts' comments unread from the one written at `at`: that
 * comment and every later one on the artifact need the member again.
 */
export const markCommentsUnread = (items: { artifactId: string; at: string }[]) =>
  commentReadAPI.set(
    items.map((i) => ({ artifact_id: i.artifactId, read_at: new Date(Date.parse(i.at) - 1).toISOString() })),
  );

/**
 * Read state where an artifact's comments are read (ArtifactHistory): which
 * comments are new to the member, marking them read once they are seen, and
 * Mark unread on any one of them. Seen is at once for the Notes panel; for
 * the section at the foot of the artifact's pane, when a link asked for the
 * notes or the section scrolls into view. A comment marked unread stays so
 * for as long as the artifact stays open, so reading on does not undo it.
 */
export const useNoteReads = ({
  artifactId,
  entries,
  enabled,
  userId,
  seenNow,
  observe,
}: {
  artifactId: string;
  entries: ChatterEntry[];
  enabled: boolean;
  userId?: string | null;
  seenNow: boolean;
  observe?: React.RefObject<HTMLElement | null>;
}) => {
  // undefined while loading, null for never read.
  const [readAt, setReadAt] = useState<number | null | undefined>(undefined);
  const [kept, setKept] = useState(false);
  const [seen, setSeen] = useState(false);
  const marked = useRef(false);

  useEffect(() => {
    setReadAt(undefined);
    setKept(false);
    setSeen(false);
    marked.current = false;
    if (!enabled) return undefined;
    let live = true;
    loadCommentReads([artifactId])
      .then((m) => live && setReadAt(m.get(artifactId) ?? null))
      .catch(() => live && setReadAt(null));
    return () => {
      live = false;
    };
  }, [artifactId, enabled]);

  useEffect(() => {
    if (!enabled) return undefined;
    if (seenNow) {
      setSeen(true);
      return undefined;
    }
    const el = observe?.current;
    if (!el || typeof IntersectionObserver === 'undefined') return undefined;
    const io = new IntersectionObserver((seenEntries) => {
      if (seenEntries.some((e) => e.isIntersecting)) setSeen(true);
    });
    io.observe(el);
    return () => io.disconnect();
  }, [enabled, seenNow, observe, artifactId]);

  /** Someone else's comment: the only kind that can be new to the member. */
  const theirs = useCallback(
    (e: ChatterEntry) => !e.is_auto_entry && e.entry_type === 'comment' && !(userId && e.created_by === userId),
    [userId],
  );
  const isNew = useCallback(
    (e: ChatterEntry) =>
      enabled && readAt !== undefined && theirs(e) && (readAt === null || Date.parse(e.created_at) > readAt),
    [enabled, readAt, theirs],
  );
  const anyNew = entries.some(isNew);

  useEffect(() => {
    if (!enabled || !seen || kept || marked.current || !anyNew) return;
    marked.current = true;
    markCommentsRead([artifactId]).catch(() => {
      marked.current = false;
    });
  }, [enabled, seen, kept, anyNew, artifactId]);

  const markUnread = async (e: ChatterEntry) => {
    await markCommentsUnread([{ artifactId, at: e.created_at }]);
    setKept(true);
    setReadAt(Date.parse(e.created_at) - 1);
  };

  return { enabled, theirs, isNew, kept, markUnread };
};
