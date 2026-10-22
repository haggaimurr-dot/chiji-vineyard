import { callApi } from './api'

export type RestockConfig = { enabled: boolean; templateId: string; demo?: boolean }

// Call directly from a tap handler using config fetched when the page loads.
export async function requestRestock(config: RestockConfig | null): Promise<boolean> {
  if (!config?.enabled || config.demo) return false
  try {
    const result = await wx.requestSubscribeMessage({ tmplIds: [config.templateId] })
    return result[config.templateId] === 'accept'
  } catch { return false }
}

export async function saveRestockGrant(productId: string, config: RestockConfig) {
  return callApi<{ status: string }>('product.restockSubscription.accept', { productId, templateId: config.templateId })
}

export async function savePriceDropGrant(productId: string, config: RestockConfig) {
  return callApi<{ status: string }>('product.priceDropSubscription.accept', { productId, templateId: config.templateId })
}
