import {
  motion,
  useInView,
  useMotionValue,
  useTransform,
  useSpring,
} from "framer-motion";
import { useRef } from "react";

const DEMO_URL =
  (import.meta.env.VITE_DEMO_URL as string | undefined)?.trim() || "/demo";

interface CardDef {
  src: string;
  alt: string;
  clipHeight: number;
  float: { duration: number; delay: number; amount: number };
  enter: { delay: number; x: number; y: number };
}

const CARDS: CardDef[] = [
  {
    src: "/screenshots/overview.png",
    alt: "CyberMesh Overview — Backend Readiness, AI Service, Network Health, API Throughput",
    clipHeight: 440,
    float: { duration: 5.2, delay: 0, amount: 9 },
    enter: { delay: 0, x: -28, y: 16 },
  },
  {
    src: "/screenshots/ai-engine-detail.png",
    alt: "CyberMesh AI Engine — Loop Status Running 15.7 detections/min, 99% publish success",
    clipHeight: 440,
    float: { duration: 6.5, delay: 1.1, amount: 12 },
    enter: { delay: 0.13, x: 28, y: 16 },
  },
  {
    src: "/screenshots/network-topology.png",
    alt: "CyberMesh Consensus Network — 5-node cluster PBFT topology visualization",
    clipHeight: 360,
    float: { duration: 4.8, delay: 0.5, amount: 8 },
    enter: { delay: 0.24, x: -28, y: 20 },
  },
  {
    src: "/screenshots/threat-table.png",
    alt: "CyberMesh Threat Intelligence — aggregated AI detections by threat type",
    clipHeight: 360,
    float: { duration: 5.9, delay: 1.7, amount: 11 },
    enter: { delay: 0.35, x: 0, y: 24 },
  },
  {
    src: "/screenshots/blockchain-detail.png",
    alt: "CyberMesh Blockchain — 101,452 blocks, 377,513 transactions, 98% success rate",
    clipHeight: 360,
    float: { duration: 7.1, delay: 0.8, amount: 7 },
    enter: { delay: 0.46, x: 28, y: 20 },
  },
];

const ScreenshotFrame = ({
  src,
  alt,
  clipHeight,
}: {
  src: string;
  alt: string;
  clipHeight: number;
}) => (
  <div
    className="rounded-xl overflow-hidden border border-black/[0.07] transition-shadow duration-500 group-hover:shadow-[0_28px_72px_-14px_rgba(0,0,0,0.17),0_8px_28px_-6px_rgba(0,0,0,0.09)]"
    style={{
      boxShadow:
        "0 12px 40px -10px rgba(0,0,0,0.10), 0 4px 14px -4px rgba(0,0,0,0.06), 0 0 0 1px rgba(0,0,0,0.04)",
    }}
  >
    <div className="flex items-center gap-1.5 px-3.5 py-2.5 bg-[#f3f3f3] border-b border-black/[0.07]">
      <span className="w-2.5 h-2.5 rounded-full bg-[#ff5f57]" />
      <span className="w-2.5 h-2.5 rounded-full bg-[#febc2e]" />
      <span className="w-2.5 h-2.5 rounded-full bg-[#28c840]" />
      <div className="ml-2 flex-1 max-w-[180px]">
        <div className="bg-white border border-black/[0.09] rounded text-[10px] text-gray-400 px-2.5 py-[3px] text-center leading-[1.6] tracking-wide">
          cybermesh.qzz.io
        </div>
      </div>
    </div>
    <div style={{ maxHeight: clipHeight, overflow: "hidden" }}>
      <img
        src={src}
        alt={alt}
        loading="lazy"
        style={{ width: "100%", display: "block" }}
      />
    </div>
  </div>
);

const FloatingCard = ({
  card,
  inView,
}: {
  card: CardDef;
  inView: boolean;
}) => {
  const mouseX = useMotionValue(0);
  const mouseY = useMotionValue(0);
  const rawRX = useTransform(mouseY, [-0.5, 0.5], [3.5, -3.5]);
  const rawRY = useTransform(mouseX, [-0.5, 0.5], [-3.5, 3.5]);
  const rotateX = useSpring(rawRX, { stiffness: 180, damping: 22 });
  const rotateY = useSpring(rawRY, { stiffness: 180, damping: 22 });

  return (
    <motion.div
      initial={{ opacity: 0, x: card.enter.x, y: card.enter.y }}
      animate={inView ? { opacity: 1, x: 0, y: 0 } : {}}
      transition={{
        duration: 0.8,
        delay: card.enter.delay,
        ease: [0.22, 0.58, 0.32, 1.0],
      }}
    >
      <motion.div
        animate={{ y: [0, -card.float.amount, 0] }}
        transition={{
          duration: card.float.duration,
          delay: card.float.delay,
          repeat: Infinity,
          ease: "easeInOut",
        }}
      >
        <motion.div
          className="group cursor-default"
          style={{ rotateX, rotateY, transformPerspective: 1000 }}
          onMouseMove={(e) => {
            const r = e.currentTarget.getBoundingClientRect();
            mouseX.set((e.clientX - r.left) / r.width - 0.5);
            mouseY.set((e.clientY - r.top) / r.height - 0.5);
          }}
          onMouseLeave={() => {
            mouseX.set(0);
            mouseY.set(0);
          }}
        >
          <ScreenshotFrame
            src={card.src}
            alt={card.alt}
            clipHeight={card.clipHeight}
          />
        </motion.div>
      </motion.div>
    </motion.div>
  );
};

const ProductGallerySection = () => {
  const ref = useRef(null);
  const inView = useInView(ref, { once: true, margin: "-80px" });

  const [c0, c1, c2, c3, c4] = CARDS;

  return (
    <section
      ref={ref}
      className="py-24 px-4 sm:px-6 overflow-hidden"
      style={{ background: "hsl(220 18% 97%)" }}
    >
      <div className="mx-auto max-w-6xl">

        <motion.div
          initial={{ opacity: 0, y: 20 }}
          animate={inView ? { opacity: 1, y: 0 } : {}}
          transition={{ duration: 0.55 }}
          className="mb-12"
        >
          <div className="flex flex-col sm:flex-row sm:items-end justify-between gap-6">
            <div>
              <h2 className="text-3xl sm:text-4xl font-display font-bold tracking-tight text-primary leading-tight">
                Live. Not a mockup.
              </h2>
              <p className="mt-3 text-sm text-muted-foreground max-w-md leading-relaxed">
                Every screen below is the actual CyberMesh dashboard, running against real infrastructure today.
              </p>
            </div>
            <a
              href={DEMO_URL}
              className="landing-btn-ghost self-start sm:self-auto flex-shrink-0 text-sm"
            >
              Explore the live demo →
            </a>
          </div>
        </motion.div>

        <div className="grid grid-cols-1 lg:grid-cols-[57fr_43fr] gap-4 sm:gap-5 mb-4 sm:mb-5">
          <FloatingCard card={c0} inView={inView} />
          <FloatingCard card={c1} inView={inView} />
        </div>

        <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 gap-4 sm:gap-5">
          <FloatingCard card={c2} inView={inView} />
          <FloatingCard card={c3} inView={inView} />
          <FloatingCard card={c4} inView={inView} />
        </div>

      </div>
    </section>
  );
};

export default ProductGallerySection;
