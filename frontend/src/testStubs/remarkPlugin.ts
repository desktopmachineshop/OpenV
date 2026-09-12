/**
 * A remark plugin that does nothing, for the Jest runs that cannot import the
 * real ESM ones. The markdown stub renders its source verbatim, so no plugin
 * it is handed would have anything to transform anyway.
 */
const noopPlugin = () => () => undefined;

export default noopPlugin;
