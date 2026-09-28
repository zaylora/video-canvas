import service from '@/utils/requests/service'
import type { AssetDto } from './type'

export const uploadAsset = (file: File) => {
  const formData = new FormData()
  formData.append('file', file)
  return service.upload<AssetDto>('/assets', formData)
}
