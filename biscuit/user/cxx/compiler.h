#define __XCONCAT2(a, b) a ## b
#define __XCONCAT(a, b) __XCONCAT2(a, b)

#define CACHELINE	64

#define __padout__  \
  char __XCONCAT(__padout, __COUNTER__)[0] __attribute__((aligned(CACHELINE)))
#define __mpalign__ __attribute__((aligned(CACHELINE)))
#define __noret__   __attribute__((noreturn))
#define barrier() __asm volatile("" ::: "memory")

//#ifdef __cplusplus
//#define BEGIN_DECLS extern "C" {
//#define END_DECLS   }
//#else
//#define BEGIN_DECLS
//#define END_DECLS
//#endif
