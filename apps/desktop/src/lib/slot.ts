/**
 * One value handed from an event to whoever waits for it: a stream of one chunk, read once. A
 * value put after the first is dropped.
 */
export const slot = <T>() => {
  let controller: ReadableStreamDefaultController<T> | undefined;
  const stream = new ReadableStream<T>({
    start: (c) => {
      controller = c;
    },
  });
  return {
    put: (value: T) => {
      controller?.enqueue(value);
      controller?.close();
      controller = undefined;
    },
    /** The value; undefined when the stream ended without one. */
    take: async (): Promise<T | undefined> => {
      const read = await stream.getReader().read();
      return read.value;
    },
  };
};
