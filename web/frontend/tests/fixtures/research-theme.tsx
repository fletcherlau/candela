import React from "react";
import { createRoot } from "react-dom/client";
import { ResearchTheme } from "@/components/research-theme";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Checkbox } from "@/components/ui/checkbox";
import { Card, CardHeader, CardTitle, CardContent } from "@/components/ui/card";
import { Dialog, DialogTrigger, DialogContent, DialogHeader, DialogTitle, DialogDescription } from "@/components/ui/dialog";
import "@/style.css";

function ExampleDialog({ label }: { label: string }) {
  return <Dialog><DialogTrigger asChild><Button>{label}</Button></DialogTrigger><DialogContent><DialogHeader><DialogTitle>{label}</DialogTitle><DialogDescription>验证主题跨越 Portal 后保持一致。</DialogDescription></DialogHeader><Input aria-label="弹窗输入" /></DialogContent></Dialog>;
}
createRoot(document.getElementById("root")!).render(<>
  <ResearchTheme data-testid="research">
    <Card><CardHeader><CardTitle>研究主题</CardTitle></CardHeader><CardContent>
      <Button>研究按钮</Button><Button variant="outline">次要按钮</Button><Button variant="secondary">中性按钮</Button><Button variant="destructive">错误按钮</Button>
      <Input aria-label="研究输入" /><Checkbox aria-label="研究选择" />
      <ExampleDialog label="研究弹窗" />
    </CardContent></Card>
  </ResearchTheme>
  <section data-testid="legacy"><Button>原有按钮</Button><ExampleDialog label="原有弹窗" /></section>
</>);
