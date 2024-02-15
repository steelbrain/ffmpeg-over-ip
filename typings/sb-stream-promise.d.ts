declare module 'sb-stream-promise' {
  export default function streamToPromise(
    stream: NodeJS.ReadableStream,
    bytesLimit?: number
  ): Promise<string>
}
