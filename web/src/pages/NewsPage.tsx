import { NewsTab } from '../components/NewsTab'

export function NewsPage() {
  return (
    <div className="min-h-screen bg-nofx-bg pt-16 px-4 sm:px-6 lg:px-8 pb-4">
      <div className="max-w-7xl mx-auto">
        <NewsTab />
      </div>
    </div>
  )
}
