#import <Cocoa/Cocoa.h>

void *aiofcage_clipboard_image(int limit, int *length) {
    __block void *result = NULL;
    __block int size = 0;
    void (^readImage)(void) = ^{
        @autoreleasepool {
            NSPasteboard *board = [NSPasteboard generalPasteboard];
            NSData *data = [board dataForType:NSPasteboardTypePNG];
            if (!data) data = [board dataForType:@"public.jpeg"];
            if (!data) {
                NSData *tiff = [board dataForType:NSPasteboardTypeTIFF];
                if (tiff) {
                    if (tiff.length > limit) { size = -1; return; }
                    NSBitmapImageRep *rep = [NSBitmapImageRep imageRepWithData:tiff];
                    if (rep.pixelsWide > 8192 || rep.pixelsHigh > 8192 || (long long)rep.pixelsWide * rep.pixelsHigh > 20000000) { size = -1; return; }
                    data = [rep representationUsingType:NSBitmapImageFileTypePNG properties:@{}];
                }
            }
            if (data.length > limit) { size = -1; return; }
            if (data.length) {
                result = malloc(data.length);
                if (result) { memcpy(result, data.bytes, data.length); size = (int)data.length; }
            }
        }
    };
    if ([NSThread isMainThread]) readImage();
    else dispatch_sync(dispatch_get_main_queue(), readImage);
    *length = size;
    return result;
}
