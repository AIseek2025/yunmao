import Link from "next/link";

export default function Home() {
  return (
    <main className="min-h-screen flex flex-col items-center justify-center gap-6 p-6">
      <h1 className="text-3xl font-bold">yunmao 云养猫</h1>
      <p className="text-neutral-500">
        24×7 户外猫直播 + 远程投喂 + 弹幕互动。
      </p>
      <div className="flex gap-3">
        <Link
          className="px-4 py-2 rounded-md bg-brand text-white hover:bg-brand-700"
          href="/login"
        >
          登录
        </Link>
        <Link
          className="px-4 py-2 rounded-md border border-neutral-300"
          href="/rooms"
        >
          浏览直播
        </Link>
      </div>
    </main>
  );
}
