export type SaleMode = 'spot' | 'presale'

export interface Sku {
  id: string
  name: string
  price: number
  compareAtPrice?: number
  stock: number
  limitPerOrder: number
  weightLabel: string
  sold?: number
}

export interface Product {
  favorite?: boolean
  restockReminder?: boolean
  restockPushStatus?: string
  priceDropReminder?: boolean
  priceDrop?: { skuId: string; skuName: string; baselinePrice: number; targetPrice?: number; currentPrice: number; dropped: boolean; available: boolean; pushStatus: string }
  id: string
  name: string
  subtitle: string
  category: 'family' | 'gift'
  saleMode: SaleMode
  expectedShipDate?: string
  cover: string
  imageNote?: string
  gallery: string[]
  badges: string[]
  origin: string
  variety?: string
  storage?: string
  netWeightNote?: string
  description: string
  highlights: string[]
  traceability?: Traceability
  giftCustomization?: GiftCustomization
  pairings?: Pairing[]
  skus: Sku[]
}

export interface ProductSearchResult { items: Product[]; total: number; offset: number; limit: number }

export interface Traceability { orchard: string; batch: string; code?: string; harvestedAt: string; packedAt?: string; packing: string; steps: Array<{ title: string; detail: string }>; demo?: boolean }
export interface GiftCustomization { enabled: boolean; description: string; options: string[]; minimumQuantity: number; leadTime: string }
export interface Pairing { title: string; description: string; productId?: string }
export interface Review { status?: 'PENDING' | 'APPROVED' | 'REJECTED'; rejectReason?: string; moderatedAt?: string; images?: string[]; appendContent?: string; appendImages?: string[]; appendAt?: string; reply?: string; replyAt?: string; id: string; productId: string; rating: number; content: string; createdAt: string; demo?: boolean }
export interface ReviewSummary { total: number; average: number; items: Review[]; page: number; pageSize: number; hasMore: boolean }

export interface CartItem {
  name?: string
  skuName?: string
  cover?: string
  productId: string
  skuId: string
  quantity: number
  selected: boolean
  product?: Product
  sku?: Sku
}

export interface Address {
  id?: string
  isDefault?: boolean
  name: string
  phone: string
  province: string
  city: string
  district: string
  detail: string
}

export interface Invoice { autoStatus?: string; autoError?: string; kind: string; title: string; taxId: string; email: string; status: string; number?: string; reason?: string; amount?: number; fileName?: string; createdAt?: string; issuedAt?: string; emailStatus?: string; emailError?: string; sentAt?: string }

export interface PriceQuote {
  shippingRuleName?: string
  surcharge?: number
  deliveryMin?: string
  deliveryMax?: string
  discount?: number
  couponCode?: string
  subtotal: number
  freight: number
  total: number
  freeShippingThreshold: number
  blockedReason?: string
}

export interface PackageItem { itemIndex: number; quantity: number }
export interface OrderPackage { id: string; number: string; items: PackageItem[]; shipment?: {carrier: string; carrierCode?: string; trackingNo: string; shippedAt?: string; syncStatus?: string; syncError?: string}; receivedAt?: string }
export interface OrderEvent { id: string; kind: string; detail: string; actor: string; packageId?: string; createdAt: string }
export interface Order {
 packages?: OrderPackage[]
 packageVersion?: number
 events?: OrderEvent[]
 batches?: Array<{skuId:string;batchId:string;quantity:number;batch:{id:string;productId:string;code:string;number:string;orchard:string;harvestedAt:string;packedAt:string;expiresAt:string}}>
 batchVersion?: number
  paidAt?: string
  refundedAmount?: number
  deliveryDate?: string
  invoice?: Invoice
  id: string
  orderNo: string
  createdAt: string
  orderStatus: 'PENDING_PAYMENT' | 'CONFIRMED' | 'CANCELLED' | 'CLOSED' | 'COMPLETED'
  paymentStatus: 'UNPAID' | 'PAID' | 'REFUNDING' | 'REFUNDED'
  fulfillmentStatus: 'UNFULFILLED' | 'PREPARING' | 'SHIPPED' | 'RECEIVED'
  refundStatus: 'NONE' | 'APPLIED' | 'APPROVED' | 'REJECTED' | 'SUCCEEDED' | 'FAILED' | 'PARTIAL'
  items: CartItem[]
  address: Address
  quote: PriceQuote
  note?: string
  shipment?: { parcels?: Parcel[]; carrier: string; trackingNo: string; shippedAt?: string }
}

export interface OrderSearchResult { items: Order[]; total: number; offset: number; limit: number }

export interface ShippingNotice { parcels?: Parcel[]; orderId: string; orderNo: string; carrier: string; trackingNo: string; shippedAt: string; read: boolean }
export interface ShippingSubscription { enabled: boolean; templateId: string; status: string; demo?: boolean }

export interface Article {
  completed?: boolean
  favorite?: boolean
  lastReadAt?: string
  lastReadText?: string
  id: string
  title: string
  summary: string
  cover: string
  variety: string
  stage: string
  type: 'article' | 'video'
  duration?: string
  publishedAt: string
  content: string[]
  contentHtml?: string
  videoUrl?: string
  status?: 'DRAFT' | 'SCHEDULED' | 'PUBLISHED'
  version?: number
  scheduledAt?: string
  updatedAt?: string
  readCount?: number
  readerCount?: number
}

export interface UserProfile {
  id: string
  nickname: string
  avatarUrl?: string
}

export interface ApiResponse<T> {
  ok: boolean
  data: T
  requestId?: string
}


export interface CustomerService { enabled:boolean;wechat:boolean;phone:string;hours:string;responseTime:string }
export interface HomeConfig {
 live?: { orchard: OrchardLiveChannel; stall: OrchardLiveChannel }
 customerService?: CustomerService
 activities?: Array<{id?:string;reservationEnabled?:boolean;capacity?:number;waitlistEnabled?:boolean;cover?:string;location?:string;title:string;summary:string;content:string;startDate:string;endDate:string;productId:string}>
 announcements?: Array<{title:string;content:string;publishedAt:string}>
 launchCalendar?: Array<{variety:string;startDate:string;endDate:string;note:string;productId:string}>
 banners: Array<{title:string;copy:string;image:string;button:string;target:string;targetId:string}>
 productIds:string[]
 articleIds:string[]
 storyTitle:string
 storyCopy:string
 cooperationTitle:string
 cooperationCopy:string
}
export interface OrchardLiveChannel { enabled:boolean; url:string; hours:string; noticeAt?:string; noticeText?:string }
export interface PrivacyPolicy {title:string;version:string;effectiveDate:string;operator:string;contact:string;address:string;published:boolean;content:string[]}
export interface Tracking {state?:string;status:string;message:string;updatedAt:string;events:Array<{time:string;context:string}>}

export interface Refund {
  providerRefundId?: string
  orderTotal?: number
  fulfillmentStatus?: string

  images?: string[]; id: string; orderId: string; orderNo: string; amount: number; reason: string; note?: string; status: string; rejectReason?: string; createdAt: string; updatedAt?: string }

export interface Parcel { carrier: string; carrierCode?: string; trackingNo: string; shippedAt?: string }
