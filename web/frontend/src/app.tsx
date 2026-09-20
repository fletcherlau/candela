import { lazy, Suspense } from "react";
import { ArrowRight, ChartNoAxesCombined, Database, Leaf } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  NavigationMenu,
  NavigationMenuContent,
  NavigationMenuItem,
  NavigationMenuLink,
  NavigationMenuList,
  NavigationMenuTrigger,
} from "@/components/ui/navigation-menu";
import { Rotation } from "@/pages/rotation";
import { DataManagement } from "@/pages/data-management";

const DesignPreview = lazy(() =>
  import("@/pages/design-preview").then((module) => ({
    default: module.DesignPreview,
  })),
);

function Brand() {
  return (
    <a
      href="/"
      aria-label="Candela 首页"
      className="inline-flex shrink-0 items-center gap-2 text-2xl font-semibold tracking-tight"
    >
      Candela<span className="text-primary">.</span>
    </a>
  );
}

function Header() {
  return (
    <header className="border-b bg-white">
      <div className="mx-auto flex h-20 max-w-6xl items-center gap-10 px-6 sm:px-8">
        <Brand />
        <NavigationMenu aria-label="主要导航" viewport={false}>
          <NavigationMenuList>
            <NavigationMenuItem>
              <NavigationMenuTrigger className="text-sm">
                策略
              </NavigationMenuTrigger>
              <NavigationMenuContent className="w-52! p-2">
                <NavigationMenuLink
                  asChild
                  active={location.pathname === "/market"}
                >
                  <a
                    href="/market"
                    aria-current={
                      location.pathname === "/market" ? "page" : undefined
                    }
                  >
                    <span className="font-medium">市场状态</span>
                    <span className="text-xs text-muted-foreground">
                      观察全市场走势与市场环境
                    </span>
                  </a>
                </NavigationMenuLink>
                <NavigationMenuLink
                  asChild
                  active={location.pathname === "/strategies/four-etf-rotation"}
                >
                  <a
                    href="/strategies/four-etf-rotation"
                    aria-current={
                      location.pathname === "/strategies/four-etf-rotation"
                        ? "page"
                        : undefined
                    }
                  >
                    <span className="font-medium">四标的轮动</span>
                    <span className="text-xs text-muted-foreground">
                      查看收益、回撤与持仓变化
                    </span>
                  </a>
                </NavigationMenuLink>
              </NavigationMenuContent>
            </NavigationMenuItem>
          </NavigationMenuList>
        </NavigationMenu>
      </div>
    </header>
  );
}

function Home() {
  return (
    <div className="py-16 sm:py-24">
      <p className="mb-5 text-xs font-medium tracking-[0.22em] text-primary">
        CANDELA · 市场与策略
      </p>
      <h1 className="max-w-3xl text-4xl leading-tight font-semibold tracking-tight sm:text-6xl">
        看清市场，
        <br />
        让决策更有依据。
      </h1>
      <p className="mt-6 max-w-xl text-base leading-8 text-muted-foreground">
        从市场环境出发，理解策略的选择与表现。
      </p>
      <Button asChild size="lg" className="mt-8">
        <a href="/market">
          查看市场状态 <ArrowRight aria-hidden="true" />
        </a>
      </Button>
      <Card className="mt-20 max-w-2xl shadow-none">
        <CardHeader>
          <ChartNoAxesCombined
            aria-hidden="true"
            className="mb-3 size-6 text-primary"
          />
          <CardTitle>市场状态</CardTitle>
          <CardDescription>
            通过中证全指观察沪深 A 股的整体市场环境。
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Badge variant="secondary">市场图表准备中</Badge>
        </CardContent>
      </Card>
    </div>
  );
}

function Market() {
  return (
    <div className="space-y-8 py-10 sm:py-14">
      <div>
        <p className="mb-3 text-xs tracking-[0.2em] text-primary">
          MARKET STATE
        </p>
        <h1 className="text-3xl font-semibold">市场状态</h1>
        <p className="mt-3 text-sm leading-6 text-muted-foreground">
          从全市场走势，观察策略所处的市场环境。
        </p>
      </div>
      <Card className="shadow-none">
        <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-4 border-b">
          <div>
            <CardTitle className="text-lg">
              中证全指{" "}
              <span className="ml-2 text-xs font-normal text-muted-foreground">
                000985.CSI
              </span>
            </CardTitle>
            <CardDescription className="mt-2">
              沪深 A 股整体市场表现
            </CardDescription>
          </div>
          <Badge variant="secondary">准备中</Badge>
        </CardHeader>
        <CardContent className="flex min-h-80 flex-col items-center justify-center px-6 py-16 text-center">
          <ChartNoAxesCombined
            aria-hidden="true"
            className="mb-6 size-10 text-primary/60"
          />
          <h2 className="text-xl font-medium">市场图表准备中</h2>
          <p className="mt-3 max-w-md text-sm leading-7 text-muted-foreground">
            市场走势与状态分析正在准备，完成后可在这里查看。
          </p>
        </CardContent>
      </Card>
      <div className="grid gap-5 sm:grid-cols-2">
        <Card className="shadow-none">
          <CardHeader>
            <CardTitle className="text-base">正式市场状态</CardTitle>
            <CardDescription>依据已完成月份的数据确认。</CardDescription>
          </CardHeader>
          <CardContent>
            <span className="text-sm text-muted-foreground">等待状态分析</span>
          </CardContent>
        </Card>
        <Card className="shadow-none">
          <CardHeader>
            <CardTitle className="text-base">当月暂算状态</CardTitle>
            <CardDescription>
              反映当月变化，与正式状态分别展示。
            </CardDescription>
          </CardHeader>
          <CardContent>
            <span className="text-sm text-muted-foreground">等待状态分析</span>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

function Admin() {
  return (
    <div className="min-h-screen">
      <header className="border-b bg-white">
        <div className="mx-auto flex h-20 max-w-7xl items-center justify-between px-6">
          <Brand />
          <Badge variant="outline">管理</Badge>
        </div>
      </header>
      <div className="mx-auto grid max-w-7xl md:grid-cols-[180px_minmax(0,1fr)]">
        <aside className="border-b p-6 md:min-h-[calc(100vh-5rem)] md:border-r md:border-b-0">
          <nav aria-label="管理导航">
            <Button
              asChild
              variant="secondary"
              className="w-full justify-start"
            >
              <a href="/admin/data" aria-current="page">
                <Database aria-hidden="true" />
                数据管理
              </a>
            </Button>
          </nav>
        </aside>
        <main id="main" tabIndex={-1} className="min-w-0 px-6 py-10">
          <DataManagement />
        </main>
      </div>
    </div>
  );
}

export function App() {
  if (location.pathname === "/design-preview") {
    return (
      <Suspense fallback={<p role="status">正在打开设计样板…</p>}>
        <DesignPreview />
      </Suspense>
    );
  }
  const admin =
    location.pathname === "/admin" || location.pathname === "/admin/data";
  return (
    <>
      <a
        href="#main"
        className="sr-only fixed top-2 left-2 z-50 rounded bg-white p-3 focus:not-sr-only"
      >
        跳转到主要内容
      </a>
      {admin ? (
        <Admin />
      ) : (
        <div className="flex min-h-screen flex-col">
          <Header />
          <main
            id="main"
            tabIndex={-1}
            className="mx-auto w-full max-w-6xl flex-1 px-6 sm:px-8"
          >
            {location.pathname === "/market" ? (
              <Market />
            ) : location.pathname === "/strategies/four-etf-rotation" ? (
              <Rotation />
            ) : (
              <Home />
            )}
          </main>
          <footer className="border-t">
            <div className="mx-auto flex max-w-6xl flex-wrap items-center justify-between gap-3 px-6 py-6 text-xs text-muted-foreground sm:px-8">
              <span>Candela · 让数据照亮决策</span>
              <span className="inline-flex items-center gap-2">
                <Leaf aria-hidden="true" className="size-3" />
                市场与策略
              </span>
            </div>
          </footer>
        </div>
      )}
    </>
  );
}
