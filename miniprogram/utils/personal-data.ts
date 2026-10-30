export const closedKey = 'chiji_account_closed'
const addressKeys = ['chiji_demo_addresses', 'chiji_default_address']
const usageKeys = ['chiji_demo_commerce_activity', 'chiji_demo_cart', 'chiji_checkout_items', 'chiji_demo_attention', 'chiji_demo_article_reads', 'chiji_demo_article_favorites', 'chiji_demo_classroom_progress', 'chiji_demo_classroom_questions', 'chiji_demo_video_progress', 'chiji_demo_shipping_read', 'chiji_demo_shipping_subscriptions', 'chiji_demo_activity_notices', 'chiji_home_preferences', 'chiji_classroom_view', ...['demo', 'live'].flatMap(mode => ['product_history', 'product_compare', 'search_history', 'search_favorites'].map(kind => `chiji_${mode}_${kind}`))]
export function clearPersonalStorage(scope: string) {
  const keys = scope === 'addresses' ? addressKeys : scope === 'usage' ? usageKeys : [...addressKeys, ...usageKeys, 'chiji_mini_token', 'chiji_user_cache', 'chiji_demo_orders', 'chiji_demo_refunds', 'chiji_demo_tickets', 'chiji_demo_evidence', 'chiji_demo_reviews', 'chiji_demo_coupon_wallet', 'chiji_demo_activity_reservations', 'chiji_demo_activity_waitlist']
  keys.forEach(key => wx.removeStorageSync(key))
}
