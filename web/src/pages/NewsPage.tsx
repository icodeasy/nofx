import { NewsTab } from '../components/NewsTab'

export function NewsPage() {
  return (
    <div className="min-h-screen bg-nofx-bg pt-16 px-4 sm:px-6 lg:px-8">
      <div className="max-w-7xl mx-auto py-8">
        <div className="h-[calc(100vh-8rem)]">
          <NewsTab />
        </div>
      </div>
    </div>
  )
}
