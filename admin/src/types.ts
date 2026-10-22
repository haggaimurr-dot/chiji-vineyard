export interface InventoryMovement {
  id: string; productId: string; productName: string; skuId: string; skuName: string
  kind: string; reference: string; username: string; reason: string; createdAt: string
  beforeStock: number; afterStock: number; beforeReserved: number; afterReserved: number; beforeSold: number; afterSold: number
}
export interface InventoryPage { items: InventoryMovement[]; total: number; limit: number; offset: number }
export interface InventoryAlert {
  skuId: string; productId: string; productName: string; skuName: string; status: string
  stock: number; reserved: number; threshold: number; enabled: boolean; alert: boolean
}

export interface Sku {
  expectedStock?: number
  id: string
  name: string
  price: number
  compareAtPrice?: number
  stock: number
  reserved?: number
  sold?: number
  limitPerOrder: number
  weightLabel: string
  active?: boolean
}

export interface Product {
  id: string
  name: string
  subtitle: string
  category: 'family' | 'gift'
  saleMode: 'spot' | 'presale'
  expectedShipDate?: string
  cover: string
  gallery: string[]
  badges: string[]
  origin: string
  variety?: string
  storage?: string
  netWeightNote?: string
  imageNote?: string
  description: string
  highlights: string[]
  traceability?: { orchard: string; batch: string; code?: string; harvestedAt: string; packedAt?: string; packing: string; steps: Array<{ title: string; detail: string }>; demo?: boolean }
  giftCustomization?: { enabled: boolean; description: string; options: string[]; minimumQuantity: number; leadTime: string }
  pairings?: Array<{ title: string; description: string; productId?: string }>
  status: 'PUBLISHED' | 'DRAFT'
  sortOrder: number
  skus: Sku[]
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
  buyerId?: string
  paidAt?: string
  shippingNoticeStatus?: string
  shippingNoticeError?: string
  refundedAmount?: number
  deliveryDate?: string
  invoice?: {autoStatus?: string; autoError?: string; autoKey?: string; kind: string; title: string; taxId: string; email: string; status: string; number?: string; reason?: string; amount: number; fileName?: string; createdAt?: string; issuedAt?: string; emailStatus?: string; emailError?: string; sentAt?: string}
  id: string
  orderNo: string
  createdAt: string
  orderStatus: string
  paymentStatus: string
  fulfillmentStatus: string
  refundStatus: string
  items: Array<{ productId?: string; skuId?: string; name: string; skuName: string; quantity: number; cover: string; unitPrice?: number }>
  address: { name: string; phone: string; province: string; city: string; district: string; detail: string }
  quote: { total: number; subtotal?: number; freight?: number; discount?: number; couponCode?: string }
  note?: string
  shipment?: { parcels?: Parcel[]; carrier: string; carrierCode: string; trackingNo: string; shippedAt?: string; syncStatus: string; syncError?: string; syncAttempt?: string; syncStartedAt?: string }
}

export interface OrderSearchResult {
  items: Order[]
  total: number
  offset: number
  limit: number
}

export interface Refund {
  providerRefundId?: string
  orderTotal?: number
  fulfillmentStatus?: string

  images?: string[]
  rejectReason?: string
  updatedAt?: string
  id: string
  orderId: string
  orderNo: string
  amount: number
  reason: string
  note?: string
  status: string
  createdAt: string
}

export interface ContentResource { id: string; name: string; kind: 'tag' | 'image' | 'video'; url?: string; note?: string; version: number }

export interface Article {
  tags?: string[]
  reviewStatus?: 'UNSUBMITTED' | 'PENDING' | 'APPROVED' | 'REJECTED'
  reviewNote?: string
  reviewedBy?: string
  reviewedAt?: string
  id: string
  title: string
  summary: string
  cover: string
  variety: string
  stage: string
  type: 'article' | 'video'
  duration?: string
  videoUrl?: string
  contentHtml?: string
  content: string[]
  status: 'PUBLISHED' | 'DRAFT' | 'SCHEDULED'
  publishedAt: string
  scheduledAt?: string
  updatedAt?: string
  version?: number
  readCount?: number
  readerCount?: number
}

export interface ArticleVersion {
  version: number
  createdAt: string
  createdBy: string
  reason: string
  article: Article
}

export interface ArticleStats {
  totalViews: number
  totalReaders: number
  todayViews: number
  todayReaders: number
  days: Array<{ date: string; views: number; readers: number }>
}

export type LeadStatus = 'NEW' | 'CONTACTED' | 'QUALIFIED' | 'WON' | 'LOST'

export interface LeadFollowUp {
  id: string
  note: string
  status: LeadStatus
  previousStatus: LeadStatus
  createdAt: string
  username: string
  legacy?: boolean
}

export interface CustomerProfile { email: string; address: string; industry: string; notes: string }
export interface LeadFunnel { stages: Array<{ status: LeadStatus; count: number; amount: number }>; total: number; amount: number; activeAmount: number; wonAmount: number; winRate: number }

export interface LeadContact { id: string; name: string; phone: string; email: string; title: string; primary: boolean }
export interface LeadContract { id: string; number: string; amount: number; signedAt: string; dueAt: string; status: 'DRAFT' | 'SIGNED' | 'CANCELLED' }
export interface LeadReceipt { id: string; contractId: string; amount: number; receivedAt: string; reference: string }
export interface LeadRecords { contacts: LeadContact[]; contracts: LeadContract[]; receipts: LeadReceipt[] }

export interface Lead {
  contacts?: LeadContact[]
  contracts?: LeadContract[]
  receipts?: LeadReceipt[]
  externalReminder?: boolean
  id: string
  organization: string
  contact: string
  phone: string
  region: string
  channel: string
  volume: string
  demand: string
  status: LeadStatus
  followUpNote: string
  customerProfile?: CustomerProfile
  opportunityAmount?: number
  followUps: LeadFollowUp[]
  createdAt: string
  ownerId: string
  ownerName: string
  reminderAt: string
  reminderDone: boolean
  version: number
}

export interface LeadListResult {
  items: Lead[]
  total: number
  channels: string[]
  regions: string[]
  limit: number
  offset: number
}

export interface AdminOwner {
  id: string
  username: string
  role: string
  active: boolean
}

export interface LeadHistory {
  contacts?: LeadContact[]
  contracts?: LeadContract[]
  receipts?: LeadReceipt[]
  externalReminder?: boolean
  id: string
  leadId: string
  actorId: string
  actorName: string
  createdAt: string
  note: string
  status: Lead['status']
  ownerId: string
  ownerName: string
  reminderAt: string
  reminderDone: boolean
  customerProfile?: CustomerProfile
  opportunityAmount?: number
  changes: Array<'owner' | 'status' | 'note' | 'reminder' | 'profile' | 'amount' | 'contacts' | 'contracts' | 'receipts' | 'externalReminder'>
}

export interface Dashboard {
  startDate: string
  endDate: string
  grossRevenue: number
  refundedAmount: number
  buyers: number
  repeatBuyers: number
  repurchaseRate: number
  trend: Array<{ date: string; grossRevenue: number; refundedAmount: number; netRevenue: number; paidOrders: number }>
  funnel: { created: number; paid: number; received: number }
  behaviorFunnel: { visitors: number; cartUsers: number; createdUsers: number; paidUsers: number; receivedUsers: number; startedAt: string | null; partial: boolean }
  revenue: number
  paidOrders: number
  pendingShipment: number
  pendingRefund: number
  newLeads: number
  products: number
  articles: number
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
export interface OrchardLiveChannel { enabled:boolean; url:string; hours:string }
export interface PrivacyPolicy {title:string;version:string;effectiveDate:string;operator:string;contact:string;address:string;published:boolean;content:string[]}
export interface Tracking {state?:string;status:string;message:string;updatedAt:string;events:Array<{time:string;context:string}>}

export interface ShipmentItem {
  orderId: string
  carrier: string
  carrierCode: string
  trackingNo: string
}

export interface BatchShipmentResult {
  results: Array<{ orderId: string; success: boolean; error?: string; shipment?: Order['shipment']; syncStatus?: string }>
  succeeded: number
  failed: number
}

export interface OrderExportResult { filename: string; content: string; count: number }

export interface Waybill {
  id: string
  orderId: string
  orderNo: string
  carrier: string
  carrierCode: string
  trackingNo: string
  labelUrl: string
  status: 'CREATING' | 'READY' | 'FAILED' | 'UNCERTAIN' | 'CANCELLING' | 'CANCEL_UNCERTAIN' | 'CANCELLED'
  error?: string
  createdAt: string
  updatedAt: string
  demo?: boolean
}

export interface WaybillConfig {
  configured: boolean
  carriers: Array<{ code: string; name: string }>
  message?: string
}

export interface WaybillLabel { dataUrl: string; mime: string; demo?: boolean }

export interface WaybillAccount {
  disabled: boolean
  partnerId: string; partnerKey: string; partnerSecret: string
  partnerIdSet?: boolean; partnerKeySet?: boolean; partnerSecretSet?: boolean
  partnerName: string; net: string; code: string; checkMan: string
  tempId: string; expType: string; payType: string
}
export interface WaybillRate {
  carrierCode: string; province: string; firstWeight: number; firstFee: number
  additionalWeight: number; additionalFee: number; surcharge: number; delivery: string
}
export interface WaybillSettings {
  revision: number; senderName: string; senderPhone: string; senderAddress: string
  accounts: Record<string, WaybillAccount>; rates: WaybillRate[]
  carriers: Array<{code: string; name: string}>; enterpriseConfigured: boolean
}
export interface WaybillQuotes {
  items: Array<{carrierCode: string; carrier: string; available: boolean; fee?: number; delivery?: string; billableWeight?: number; message?: string}>
  weight: number; province: string; revision: number; estimated: boolean; message: string
}

export type ConfigKey = 'shipping' | 'coupons' | 'home' | 'privacy'

export interface ConfigVersion {
  id: string
  key: ConfigKey
  version: number
  action: 'BASELINE' | 'SAVE' | 'ROLLBACK'
  sourceVersion: number | null
  username: string
  createdAt: string
  payload: unknown
}

export interface ConfigVersionsPage {
  items: ConfigVersion[]
  total: number
  limit: number
  offset: number
  currentVersions: Partial<Record<ConfigKey, number>>
}

export interface AuditLog {
  id: string
  adminId: string
  username: string
  action: string
  targetId: string
  detail: unknown
  createdAt: string
}

export interface AuditPage {
  items: AuditLog[]
  total: number
  limit: number
  offset: number
}

export interface Parcel { carrier: string; carrierCode?: string; trackingNo: string; shippedAt?: string }

export interface LogisticsTask {
 id: string; orderId: string; orderNo: string; carrierCode: string; trackingNo: string;
 kind: string; status: string; owner: string; note: string; version: number; updatedAt: string;
 history: Array<{actor: string; status: string; owner: string; note: string; at: string}>;
}
