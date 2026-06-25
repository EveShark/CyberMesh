interface ProductFrameProps {
  src: string;
  alt: string;
  className?: string;
  clipHeight?: number;
  showChrome?: boolean;
}

const ProductFrame = ({
  src,
  alt,
  className = "",
  clipHeight,
  showChrome = true,
}: ProductFrameProps) => {
  return (
    <div
      className={`rounded-xl overflow-hidden border border-black/[0.08] ${className}`}
      style={{
        boxShadow:
          "0 24px 64px -12px rgba(0,0,0,0.14), 0 8px 24px -8px rgba(0,0,0,0.08), 0 0 0 1px rgba(0,0,0,0.04)",
      }}
    >
      {showChrome && (
        <div className="flex items-center gap-1.5 px-4 py-3 bg-[#f5f5f5] border-b border-black/[0.07]">
          <span className="w-3 h-3 rounded-full bg-[#ff5f57]" />
          <span className="w-3 h-3 rounded-full bg-[#febc2e]" />
          <span className="w-3 h-3 rounded-full bg-[#28c840]" />
          <div className="ml-2 flex-1 max-w-[220px]">
            <div className="bg-white border border-black/10 rounded text-[11px] text-gray-400 px-3 py-0.5 text-center leading-5">
              cybermesh.qzz.io
            </div>
          </div>
        </div>
      )}
      <div
        style={clipHeight ? { maxHeight: clipHeight, overflow: "hidden" } : undefined}
      >
        <img
          src={src}
          alt={alt}
          className="w-full block"
          style={{ display: "block" }}
        />
      </div>
    </div>
  );
};

export default ProductFrame;
