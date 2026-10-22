export interface ActivityNotice {
  kind?: 'RESERVED' | 'WAITLISTED' | 'PROMOTED'; id: string; activityId: string; title: string; startDate: string; endDate: string; createdAt: string; read: boolean
}
export interface ActivityNoticeList { items: ActivityNotice[]; unread: number; hasMore: boolean }
export const demoActivityNoticeKey = 'chiji_demo_activity_notices'
export const noticeTime = (time: string) => new Date(Date.parse(time) + 8 * 3600000).toISOString().slice(0, 16).replace('T', ' ')
