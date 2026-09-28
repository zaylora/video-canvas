export interface AssetDto {
  id: string
  url: string
  kind: 'image' | 'video'
  mimeType: string
  byteSize: number
  width: number | null
  height: number | null
  durationMs: number | null
  fileName: string | null
}
